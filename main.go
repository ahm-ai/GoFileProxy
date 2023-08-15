package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	infoColor    = "\033[1;34m%s\033[0m" // Blue
	warningColor = "\033[1;33m%s\033[0m" // Yellow
	errorColor   = "\033[1;31m%s\033[0m" // Red
	cyanColor    = "\033[1;36m%s\033[0m" // Cyan
	purpleColor  = "\033[1;35m%s\033[0m" // Purple
	greenColor   = "\033[1;32m%s\033[0m" // Green
)

func coloredLogf(color, format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	coloredMsg := fmt.Sprintf(color, msg)
	log.Print(coloredMsg)
}

func getEnvAndType() (string, string, error) {
	args := make(map[string]string)

	// Parse command-line arguments
	for _, arg := range os.Args[1:] {
		parts := strings.Split(arg, "=")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("Arguments must be in the form key=value")
		}
		args[parts[0]] = parts[1]
	}

	// Get the 'env' argument
	env, ok := args["env"]
	if !ok {
		return "", "", fmt.Errorf("env argument is required")
	}

	// Get the 'type' argument
	_type, ok := args["type"]
	if !ok {
		return "", "", fmt.Errorf("type argument is required")
	}

	// Convert 'env' and 'type' to uppercase
	env = strings.ToUpper(env)
	_type = strings.ToUpper(_type)

	return env, _type, nil
}

func levenshteinDistance(s1, s2 string) int {
	m, n := len(s1), len(s2)
	matrix := make([][]int, m+1)

	// Initialize the matrix
	for i := 0; i <= m; i++ {
		matrix[i] = make([]int, n+1)
		matrix[i][0] = i
	}
	for j := 0; j <= n; j++ {
		matrix[0][j] = j
	}

	// Fill in the matrix
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}
			matrix[i][j] = min(matrix[i-1][j]+1, matrix[i][j-1]+1, matrix[i-1][j-1]+cost)
		}
	}

	return matrix[m][n]
}

func min(a, b, c int) int {
	if a < b && a < c {
		return a
	}
	if b < c {
		return b
	}
	return c
}

func findClosestFile(targetPath string) (string, error) {
	dir := filepath.Dir(targetPath)                                                       // Get the directory of targetPath
	targetBase := strings.TrimSuffix(filepath.Base(targetPath), filepath.Ext(targetPath)) // Get the base name without extension
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return "", err // Return the error if unable to read the directory
	}

	var closestFile string
	var closestRatio float64

	for _, file := range files {
		fileBase := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name())) // Get the base name without extension
		distance := levenshteinDistance(targetBase, fileBase)
		ratio := 1 - float64(distance)/float64(max(len(targetBase), len(fileBase)))

		if ratio > closestRatio {
			closestRatio = ratio
			closestFile = filepath.Join(dir, file.Name())
		}
	}

	if closestRatio > 0.9 {
		return closestFile, nil
	}

	coloredLogf(warningColor, "Ratio: %f", closestRatio)
	return "", fmt.Errorf("No close match found")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func createFolder(folderPath string, fullPath string, requestBody []byte) {
	// Create all necessary directories in the path
	if err := os.MkdirAll(folderPath, 0755); err != nil {
		fmt.Printf("Failed to create directories: %v\n", err)
		return
	}

	// Create the file
	file, err := os.Create(fullPath)
	if err != nil {
		fmt.Printf("Failed to create file: %v\n", err)
		return
	}
	defer file.Close()

	// Write the request body to the file
	_, err = file.Write(requestBody)
	if err != nil {
		fmt.Printf("Failed to write to file: %v\n", err)
		return
	}

	fmt.Printf("File created successfully with request body at %s\n", fullPath)
}

func enableCORS(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set headers to allow CORS
		w.Header().Set("Access-Control-Allow-Origin", "*") // Adjust as needed
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

		// If it's a preflight request, respond with OK
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Call the original handler
		handler(w, r)
	}
}

func main() {

	env, _type, err := getEnvAndType()
	if err != nil {
		fmt.Println(err)
		return
	}

	target, err := url.Parse("http://google.com")

	if err != nil {
		coloredLogf(errorColor, "Error parsing target URL: %v", err)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = func(r *http.Response) error {
		// Read the response body
		bodyBytes, err := ioutil.ReadAll(r.Body)
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
		// Add more cases as needed
		default:
			extension = ".txt" // Default extension
		}

		folderPath := env + "/" + _type + "/" + r.Request.URL.Path
		// Combine folder path with file name
		fullPath := filepath.Join(folderPath + extension)

		// Create all necessary directories in the path
		if err := os.MkdirAll(folderPath, 0755); err != nil {
			fmt.Printf("Failed to create directories: %v\n", err)
			return err
		}

		// Create the file
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

		fmt.Printf("File created successfully with response body at %s\n", fullPath)

		// Write the body back to the response
		r.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))
		return nil
	}

	http.HandleFunc("/", enableCORS(func(w http.ResponseWriter, r *http.Request) {
		coloredLogf(infoColor, "Received request: %s", r.URL.Path)
		coloredLogf(infoColor, "Received body: %s", r.Body)

		// Extension
		extension := ".json"
		folderPath := env + "/" + _type + "/" + r.URL.Path
		// Combine folder path with file name
		fullPath := filepath.Join(folderPath + extension)

		// Check if file exists
		closestFile, err := findClosestFile(fullPath)
		if err == nil && closestFile != "" {
			coloredLogf(greenColor, "File found for: %s", r.URL.Path)
			http.ServeFile(w, r, closestFile)
			return
		}

		proxy.ServeHTTP(w, r)
	}))

	coloredLogf(infoColor, "Starting server on http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		coloredLogf(errorColor, "Server failed: %v", err)
	}
}
