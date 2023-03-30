package main

import (
	"bufio"
	"bytes"
	"cse224/tritonhttp"
	"fmt"
	"log"
	"net/http"
)

func main() {

	virtualHosts := tritonhttp.ParseVHConfigFile("../../virtual_hosts.yaml", "../../docroot_dirs")
	s := &tritonhttp.Server{
		Addr:         ":8080",
		VirtualHosts: virtualHosts,
	}
	go s.ListenAndServe()

	// req := fmt.Sprint("")

	req := fmt.Sprint("GET / HTTP/1.1\r\n"+
		"Host: website1\r\n",
		"Connection: close\r\n",
		"User-Agent: gotest2\r\n",
		"\r\n")

	testCase(req)

}

func testCase(req string) {
	respbytes, _, err := tritonhttp.Fetch("localhost", "8080", []byte(req))
	if err != nil {
		log.Fatalf("Error fetching request: %v\n", err.Error())
	}

	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(respbytes)), nil)

	if err != nil {
		log.Fatalf("got an error parsing the response: %v\n", err.Error())
	}

	if resp.Proto != "HTTP/1.1" {
		log.Fatalf("Expected HTTP/1.1 but got a version: %v\n", resp.Proto)
	}

	// if resp.StatusCode != 200 {
	// 	log.Fatalf("Expected response code of 200 but got: %v\n", resp.StatusCode)
	// }

	fmt.Println(string(respbytes))
	fmt.Println(resp.Header)

	log.Println("Passed...")

}
