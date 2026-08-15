package worker

import "net/http"

// fetchForecast calls a third-party HTTP service by literal host.
func fetchForecast() (*http.Response, error) {
	return http.Get("https://api.weather.example/v1/forecast")
}
