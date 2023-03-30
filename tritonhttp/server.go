package tritonhttp

import (
	"errors"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	// Addr specifies the TCP address for the server to listen on,
	// in the form "host:port". It shall be passed to net.Listen()
	// during ListenAndServe().
	Addr string // e.g. ":0"

	// VirtualHosts contains a mapping from host name to the docRoot path
	// (i.e. the path to the directory to serve static files from) for
	// all virtual hosts that this server supports
	VirtualHosts map[string]string
}

func sendBadResponse(conn net.Conn) {
	response := "HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n"
	respBytes := []byte(response)

	_, err := conn.Write(respBytes)
	if err != nil {
		log.Printf("Error writing to web server: %v\n", err.Error())
	}
}

func (s *Server) handleConnection(conn net.Conn) {

	defer conn.Close()

	buf := make([]byte, 1024)
	remaining := ""

	for {

		for strings.Contains(remaining, "\r\n\r\n") {
			idx := strings.Index(remaining, "\r\n\r\n")

			singleRequest := strings.Clone(remaining[:idx])

			request, statusCode := handleHttpRequest(singleRequest)
			if statusCode == int(400) {
				sendBadResponse(conn)
				conn.Close()
				return
			}

			// generate HTTP response
			response := s.generateHTTPResponse(request, statusCode)

			// send HTTP response
			sendHttpResponse(conn, response)

			// close TCP connection
			if request.Close {
				log.Println("Connection closed!")
				conn.Close()
				return
			}

			remaining = remaining[idx+4:]

		}

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))

		size, err := conn.Read(buf)
		if err != nil {

			if errors.Is(err, os.ErrDeadlineExceeded) {
				log.Printf("Time out by reading request from host: %v/n", err.Error())
			} else {
				log.Printf("Error reading request from host: %v/n", err.Error())
			}

			sendBadResponse(conn)
			_ = conn.Close()
			return
		}

		data := buf[:size]

		remaining = remaining + string(data)

	}

}

func handleHttpRequest(singleRequest string) (Request, int) {

	var request Request
	request.Headers = make(map[string]string)
	statusCode := 200

	i := 0
	for strings.Contains(singleRequest, "\r\n") {
		idx := strings.Index(singleRequest, "\r\n")
		requestLine := strings.Clone(singleRequest[:idx])

		if i == 0 {
			initResponseLine := strings.Fields(requestLine)
			if len(initResponseLine) != 3 {
				log.Printf("Expected to provide three components in the initial request: method, URL and version.")
				return request, 400
			} else {
				request.Method = initResponseLine[0]
				if request.Method != "GET" {
					log.Printf("Triton HTTP can only handle GET method.")
					return request, 400
				}

				request.URL = initResponseLine[1]
				if request.URL[0] != '/' {
					log.Printf("Expected to provide an URL starts with a '/' character")
					return request, 400
				}
				if request.URL[len(request.URL)-1] == '/' {
					request.URL += "index.html"
				}

				request.Proto = initResponseLine[2]
				if request.Proto != "HTTP/1.1" {
					log.Printf("Triton HTTP only has 1.1 version.")
					return request, 400
				}
			}

		} else {

			if !strings.Contains(requestLine, ":") {
				log.Printf("Expected to contains a colon inside the header lines")
				return request, 400
			} else {
				header := strings.Split(requestLine, ":")
				key := CanonicalHeaderKey(header[0])
				if key == "" { // if key is empty
					return request, 400
				}

				val := strings.Fields(header[1])[0] // get rid of space

				request.Headers[key] = val
			}

		}

		i++

		singleRequest = singleRequest[idx+2:]
	}

	// handle remaining header
	if len(singleRequest) != 0 {
		header := strings.Split(singleRequest, ":")
		if len(header) != 2 {
			log.Printf("Expected to contains a colon inside the header lines")
			return request, 400
		}

		key := CanonicalHeaderKey(header[0])
		if key == "" {
			log.Printf("Key in header should not be empty")
			return request, 400
		}

		val := strings.Fields(header[1])[0] // get rid of space

		request.Headers[key] = val
	}

	if request.Method != "GET" {
		return request, 400
	}

	host, isExist := request.Headers["Host"]
	if isExist {
		request.Host = host
	} else {
		log.Printf("Expected to provide host address")
		return request, 400
	}

	connection, isExist := request.Headers["Connection"]
	if isExist && connection == "close" {
		request.Close = true
	}

	return request, statusCode
}

func (s *Server) generateHTTPResponse(request Request, statusCode int) Response {
	var response Response
	response.Proto = "HTTP/1.1"
	response.Headers = make(map[string]string)

	date := FormatTime(time.Now())
	response.Headers["Date"] = date

	if statusCode == int(400) {
		response.StatusCode = statusCode
		response.StatusText = "Bad Request"
		response.Headers["Connection"] = "close"

	} else {

		response.Request = &request

		if request.Close {
			response.Headers["Connection"] = "close"
		}

		rootDir := s.VirtualHosts[request.Host]

		// read content
		filePath := filepath.Join(rootDir, request.URL)
		filePath = filepath.Clean(filePath)

		filePathLength := len(filePath)
		if filePathLength < len(rootDir) || filePath[:len(rootDir)] != rootDir { // path exceed root directory
			response.StatusCode = int(404)
			response.StatusText = "Not Found"
		} else {

			info, err := os.Stat(filePath)
			if err != nil {
				response.StatusCode = int(404)
				response.StatusText = "Not Found"
			} else {
				// valid http resquest
				response.StatusCode = statusCode
				response.StatusText = "OK"

				response.Headers["Last-Modified"] = FormatTime(info.ModTime())

				fileSplitByDot := strings.Split(filePath, ".")
				ext := fileSplitByDot[len(fileSplitByDot)-1]
				response.Headers["Content-Type"] = MIMETypeByExtension("." + ext)

				response.Headers["Content-Length"] = strconv.FormatInt(info.Size(), 10)

				response.FilePath = filePath
			}
		}

	}

	return response
}

func sendHttpResponse(conn net.Conn, response Response) {

	resp := response.Proto + " " + strconv.Itoa(response.StatusCode) + " " + response.StatusText + "\r\n"

	for key, val := range response.Headers {
		resp += key + ": " + val + "\r\n"
	}
	resp += "\r\n"

	respBytes := []byte(resp)
	if response.FilePath != "" {

		f, err := os.Open(response.FilePath)
		if err != nil {
			log.Printf("Error opening the requested file: %v\n", err.Error())
		} else {
			defer func() {
				if err = f.Close(); err != nil {
					log.Println(err)
				}
			}()

			data, err := ioutil.ReadAll(f)
			if err != nil {
				log.Printf("Error reading the requested file: %v\n", err.Error())
			}
			respBytes = append(respBytes, data...)

		}

	}

	conn.SetWriteDeadline(time.Now().Add(4 * time.Second))
	_, err := conn.Write(respBytes)
	if err != nil {
		log.Printf("Error writing to web server: %v\n", err.Error())
	}

}

// ListenAndServe listens on the TCP network address s.Addr and then
// handles requests on incoming connections.
func (s *Server) ListenAndServe() error {

	// Hint: Validate all docRoots

	// Hint: create your listen socket and spawn off goroutines per incoming client
	l, err := net.Listen("tcp", s.Addr)
	if err != nil {
		log.Println("Failed to listen with error: ", err)
		return err
	}

	defer l.Close()

	log.Println("Listening to connections at "+"localhost"+" on port", s.Addr)

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Println("Failed to accept incoming conn with error: ", err)
			return err
		}

		go s.handleConnection(conn)

	}

}
