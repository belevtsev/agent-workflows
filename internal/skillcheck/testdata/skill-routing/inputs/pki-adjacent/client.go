package fixture

import "net/http"

const endpoint = "https://service.example.invalid/data"

func Successful(response *http.Response) bool {
	return response.StatusCode >= 200 && response.StatusCode <= 500
}
