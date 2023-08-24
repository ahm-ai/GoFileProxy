package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	infoColor      = "\033[1;34m%s\033[0m" // Blue
	warningColor   = "\033[1;33m%s\033[0m" // Yellow
	errorColor     = "\033[1;31m%s\033[0m" // Red
	cyanColor      = "\033[1;36m%s\033[0m" // Cyan
	purpleColor    = "\033[1;35m%s\033[0m" // Purple
	greenColor     = "\033[1;32m%s\033[0m" // Green
	whiteColor     = "\033[1;37m%s\033[0m" // White
	magentaColor   = "\033[1;95m%s\033[0m" // Magenta
	grayColor      = "\033[1;90m%s\033[0m" // Gray
	lightBlueColor = "\033[1;94m%s\033[0m" // Light Blue
)

var (
	env          string
	_type        string
	err          error
	cacheEnabled bool
)

func coloredLogf(color, format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	coloredMsg := fmt.Sprintf(color, msg)
	log.Print(coloredMsg)
}

func getEnvAndType() (string, string, bool, error) {
	args := make(map[string]string)

	// Parse command-line arguments
	for _, arg := range os.Args[1:] {
		parts := strings.Split(arg, "=")
		if len(parts) != 2 {
			return "", "", false, fmt.Errorf("Arguments must be in the form key=value")
		}
		args[parts[0]] = parts[1]
	}

	// Get the 'env' argument
	env, ok := args["env"]
	if !ok {
		return "", "", false, fmt.Errorf("env argument is required")
	}

	// Get the 'type' argument
	_type, ok := args["type"]
	if !ok {
		return "", "", false, fmt.Errorf("type argument is required")
	}

	// Get the 'cache' argument
	cache, ok := args["cache"]
	if !ok {
		return "", "", false, fmt.Errorf("cache argument is required")
	}

	// Convert 'env' and 'type' to uppercase
	env = strings.ToUpper(env)
	_type = strings.ToUpper(_type)

	// Convert 'cache' to boolean
	cacheEnabled, err := strconv.ParseBool(cache)
	if err != nil {
		return "", "", false, fmt.Errorf("cache argument must be true or false")
	}

	return env, _type, cacheEnabled, nil
}

func enableCORS(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}

		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, post-check=0, pre-check=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		// If it's a preflight request, respond with OK
		if r.Method == "OPTIONS" {
			if w.Header().Get("Access-Control-Allow-Origin") == "" {
				w.Header().Set("Access-Control-Allow-Origin", origin) // Allow any origin
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		// Call the original handler
		handler(w, r)
	}
}

func modifyResponse(r *http.Response, env string, _type string, uuid string) error {
	// Read the response body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}

	// Determine the file extension based on Content-Type
	contentType := r.Header.Get("Content-Type")
	extension := ""
	switch {
	case strings.Contains(contentType, "application/json"):
		extension = ".json"
	case strings.Contains(contentType, "text/html"):
		extension = ".html"
	case strings.Contains(contentType, "application/xml"):
		extension = ".xml" // Handling for XML
	default:
		extension = ".json" // Default extension
	}

	folderPath := env + "/" + _type + "/" + r.Request.Method + "/" + uuid + strings.TrimSuffix(r.Request.URL.Path, "/")

	resp := r.Request // Get the request from the response
	rawQuery := resp.URL.RawQuery

	queryStr := ""

	pairs := strings.Split(rawQuery, "&")
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue // Skip malformed key-value pairs
		}
		key, err := url.QueryUnescape(parts[0])
		if err != nil {
			continue // Handle error in key decoding
		}

		value, err := url.QueryUnescape(parts[1])
		if err != nil {
			continue // Handle error in value decoding
		}
		queryStr += url.QueryEscape(key) + "=" + url.QueryEscape(value) + "&"
	}

	queryStr = strings.TrimSuffix(queryStr, "&") // Remove the trailing '&'

	decodedQueryStr, _ := url.QueryUnescape(queryStr)
	if decodedQueryStr != "" {
		decodedQueryStr = "?" + decodedQueryStr
	}

	fullPath := filepath.Join(folderPath) + decodedQueryStr + extension

	coloredLogf(greenColor, "Requested Folder: "+fullPath)

	if r.Request.Method == "POST" || r.Request.Method == "PUT" {
		// Delete fullpath.
		if err := os.RemoveAll(fullPath); err != nil {
			fmt.Printf("Failed to delete file: %v\n", err)
			return err
		}
	}

	segments := strings.Split(folderPath, "/")

	filteredSegments := make([]string, 0, len(segments))

	for _, segment := range segments {
		if segment != "" {
			filteredSegments = append(filteredSegments, segment)
		}
	}

	if len(filteredSegments) > 1 {
		filteredSegments = filteredSegments[:len(filteredSegments)-1]
	}

	basePath := strings.Join(filteredSegments, "/")

	if cacheEnabled {
		// Create all necessary directories in the path
		if _, err := os.Stat(basePath); os.IsNotExist(err) {
			coloredLogf(cyanColor, "Create Folder:"+basePath)
			if err := os.MkdirAll(basePath, 0755); err != nil {
				fmt.Printf("Failed to create directories: %v\n", err)
				return err
			}
		}

		// Create the file only if it doesn't exist
		file, err := os.Create(fullPath)
		if err != nil {
			fmt.Printf("Failed to create file: %v\n", err)
			return err
		}
		defer file.Close()

		// Write the response body to the file
		_, err = file.Write(bodyBytes)
		if err != nil {
			fmt.Printf("Failed to write to file: %v\n", err)
			return err
		}

	}

	// Write the body back to the response
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	return nil
}

func BuildOrderedQueryString(r *http.Request) (string, error) {
	queryValues, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return "", err
	}

	queryStr := ""
	for _, param := range strings.Split(r.URL.RawQuery, "&") {
		keyVal := strings.SplitN(param, "=", 2)
		if len(keyVal) != 2 {
			continue
		}
		key, _ := keyVal[0], keyVal[1]
		values, ok := queryValues[key]
		if ok {
			for _, v := range values {
				queryStr += key + "=" + v + "&"
			}
		}
	}

	if len(queryStr) > 0 {
		queryStr = queryStr[:len(queryStr)-1] // Remove trailing "&"
	}

	return queryStr, nil
}

func curlHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/curl_specific_path" {

		scriptPath := "./CURL/script.sh"
		output, err := exec.Command("sh", scriptPath).Output()

		coloredLogf(greenColor, "output: "+string(output))

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// w.Write(output)
		fmt.Fprintf(w, string(output)) // Send the output as a response
	}
}

func main() {

	env, _type, cacheEnabled, _ = getEnvAndType()

	target, err := url.Parse("http://localhost:8080")
	// target, err := url.Parse("https://dummyjson.com")

	if err != nil {
		coloredLogf(errorColor, "Error parsing target URL: %v", err)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	http.HandleFunc("/", enableCORS(func(w http.ResponseWriter, r *http.Request) {
		// coloredLogf(grayColor, "Received request: %s", r.URL.Path)
		coloredLogf(grayColor, "Method request: %s", r.Method)
		coloredLogf(grayColor, "Requested URL: %s", r.URL.String()) // Log the proxied URL
		coloredLogf(grayColor, "Received body: %s", r.Body)

		// Extension
		extension := ".json"

		uuid := r.Header.Get("UUID")

		if r.URL.Path == "/curl_specific_path" {
			curlHandler(w, r)
			return
		}

		proxy.ModifyResponse = func(r *http.Response) error {
			return modifyResponse(r, env, _type, uuid)
		}

		proxy.Director = func(req *http.Request) {
			req.Header.Add("User-Agent", "Curl/1.0")
			req.Host = target.Host
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = target.Path + req.URL.Path
		}
		proxy.ErrorLog = log.New(os.Stdout, "Proxy Error: ", log.LstdFlags)
		proxy.Transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			DisableKeepAlives:     false,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}

		folderPath := env + "/" + _type + "/" + r.Method + "/" + uuid + strings.TrimSuffix(r.URL.Path, "/")

		queryString, _ := BuildOrderedQueryString(r)

		if queryString != "" {
			queryString = "?" + queryString
		}

		fullPath := filepath.Join(folderPath) + queryString + extension

		if cacheEnabled {
			fileInfo, err := os.Stat(fullPath)
			if err == nil && fileInfo.Mode().IsRegular() {
				coloredLogf(greenColor, "File found for: %s", fullPath)
				w.Header().Set("Access-Control-Allow-Origin", "*") // Allow any origin
				w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
				w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

				http.ServeFile(w, r, fullPath)
				return
			}
		}

		coloredLogf(cyanColor, "Proxied URL: %s%s", target, r.URL.String()) // Log the proxied URL
		proxy.ServeHTTP(w, r)
	}))

	coloredLogf(grayColor, "Starting server on http://localhost:9090")
	if err := http.ListenAndServe(":9090", nil); err != nil {
		coloredLogf(errorColor, "Server failed: %v", err)
	}
}
