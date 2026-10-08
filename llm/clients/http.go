package clients

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// sends a request and decodes its json answer, anything but a 2xx is an error
func send(request *http.Request, answer any) error {
	body, err := fetch(request)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, answer)
}

// sends a request and returns its body, anything but a 2xx is an error
func fetch(request *http.Request) ([]byte, error) {
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return nil, fmt.Errorf("%s %s: %s: %s", request.Method, request.URL.Path, response.Status, body)
	}

	return io.ReadAll(response.Body)
}
