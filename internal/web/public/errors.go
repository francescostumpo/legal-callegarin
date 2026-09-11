package public

import (
	"net/http"
)

func writePublicError(response http.ResponseWriter, status int, message string) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Robots-Tag", "noindex, nofollow")
	http.Error(response, message, status)
}
