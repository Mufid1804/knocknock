package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

func main() {
	startTime := time.Now()

	// input file path flag
	var input string
	flag.StringVar(&input, "i", "", "set the input file (.txt) path")

	// concurrency flag
	var concurrency int
	flag.IntVar(&concurrency, "c", 20, "set the concurrency level (split equally between HTTPS and HTTP requests)")

	// timeout flag
	var to int
	flag.IntVar(&to, "t", 10000, "timeout (milliseconds), default: 10000ms")

	// possible future flag
	// redirect policy flag
	// output file flag
	flag.Parse()

	// check if input provided
	if input == "" {
		fmt.Println("Error: The 'input' are required")
		flag.PrintDefaults()
		os.Exit(2)
	}

	// create an output file
	outputFileName := strings.TrimSuffix(input, ".txt") + "-valid.txt"
	outputFile, err := os.Create(outputFileName)
	if err != nil {
		log.Fatalf("Failed to create the output file; %s", err)
	}
	defer outputFile.Close()

	writer := bufio.NewWriter(outputFile)
	defer writer.Flush()

	// make an actual time.Duration out of the timeout
	timeout := time.Duration(to) * time.Millisecond

	// setting up a custom http.Transport
	tr := &http.Transport{
		// MaxIdleConns:      30,
		// IdleConnTimeout:   time.Second,
		DisableKeepAlives: true,
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: time.Second,
		}).DialContext,
	}

	// setting up custom redirect policy
	// re := func(req *http.Request, via []*http.Request) error {
	// 	return http.ErrUseLastResponse
	// }

	// setting up main http.Client
	client := &http.Client{
		Transport: tr,
		// CheckRedirect: re,
		Timeout: timeout,
	}

	// channel for HTTP/S check
	httpsURLs := make(chan string)
	httpURLs := make(chan string)
	output := make(chan string)

	// HTTPS workers
	var httpsWG sync.WaitGroup
	for i := 0; i < concurrency/2; i++ {
		httpsWG.Add(1)

		go func() {
			defer httpsWG.Done()
			for url := range httpsURLs {
				target := "https://" + url // add protocols to domain
				if isValidResponse(client, target) {
					output <- target
				}
				httpURLs <- url
			}
		}()
	}

	// HTTP workers
	var httpWG sync.WaitGroup
	for i := 0; i < concurrency/2; i++ {
		httpWG.Add(1)

		go func() {
			defer httpWG.Done()
			for url := range httpURLs {
				target := "http://" + url // add protocols to domain
				if isValidResponse(client, target) {
					output <- target
					continue
				}
			}
		}()
	}

	// close httpURLs ch when HTTPS workers done
	go func() {
		httpsWG.Wait()
		close(httpURLs)
	}()

	// output workers
	var outputWG sync.WaitGroup
	outputWG.Add(1)
	go func() {
		for o := range output {
			_, err := writer.WriteString(o + "\n")
			fmt.Println(o)
			if err != nil {
				log.Printf("Failed to write to output file: %s", err)
			}
		}
		outputWG.Done()
	}()

	// Close output ch when HTTP workers done
	go func() {
		httpWG.Wait()
		close(output)
	}()

	// accept domain input from file path
	file, err := os.Open(input)
	if err != nil {
		log.Fatalf("Failed to open the file: %s", err)
	}
	defer file.Close()

	sc := bufio.NewScanner(file)
	for sc.Scan() {
		domain := strings.ToLower(sc.Text())
		// submit to the ch
		httpsURLs <- domain
	}

	// all done
	close(httpsURLs)

	// wait untill output done
	outputWG.Wait()

	elapsedTime := time.Since(startTime)
	fmt.Printf("Execution took %s\n", elapsedTime)
}

// check 2xx or 3xx response
func isValidResponse(client *http.Client, url string) bool {

	// create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false
	}

	req.Header.Add("Connection", "close")
	req.Close = true

	resp, err := client.Do(req)
	if resp != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		// check for 2xx and 3xx
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			return true
		}
	}

	if err != nil {
		return false
	}

	return false
}
