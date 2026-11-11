package httpx

import (
	"net/http"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/validate"
)

// BindValid decodes the request body and validates it, writing a structured
// error response and returning false when either step fails.
func BindValid(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := Bind(w, r, dst); err != nil {
		Error(w, r, err)
		return false
	}
	if details := validate.Struct(dst); len(details) > 0 {
		Error(w, r, apierr.Validation(details))
		return false
	}
	return true
}
