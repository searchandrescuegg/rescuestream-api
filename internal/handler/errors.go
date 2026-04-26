package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"alpineworks.io/rfc9457"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// WriteJSON writes a JSON response with proper error handling.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode JSON response", slog.String("error", err.Error()))
	}
}

// HTTPError represents an HTTP error with RFC 9457 problem details.
type HTTPError struct {
	Status int
	Type   string
	Title  string
	Detail string
}

func (e *HTTPError) Error() string {
	return e.Detail
}

// Error type URIs
const (
	ErrorTypeNotFound         = "/errors/not-found"
	ErrorTypeInvalidRequest   = "/errors/invalid-request"
	ErrorTypeUnauthorized     = "/errors/unauthorized"
	ErrorTypeForbidden        = "/errors/forbidden"
	ErrorTypeConflict         = "/errors/conflict"
	ErrorTypeInternalError    = "/errors/internal-error"
	ErrorTypeInvalidStreamKey = "/errors/invalid-stream-key"
	ErrorTypeStreamKeyInUse   = "/errors/stream-key-in-use"
	ErrorTypeStreamKeyRevoked = "/errors/stream-key-revoked"
	ErrorTypeStreamKeyExpired = "/errors/stream-key-expired"

	// 003-multi-tenant-platform problem types.
	ErrorTypeNoOrgMembership      = "/problems/no-org-membership"
	ErrorTypeNotInOrg             = "/problems/not-in-org"
	ErrorTypeACLDenied            = "/problems/acl-denied"
	ErrorTypeRoomArchived         = "/problems/room-archived"
	ErrorTypeStaleRoomVersion     = "/problems/stale-room-version"
	ErrorTypeDeviceKeyRevoked     = "/problems/device-key-revoked"
	ErrorTypeSessionInvalidated   = "/problems/session-invalidated"
	ErrorTypeWorkspaceDomainTaken = "/problems/workspace-domain-taken"
	ErrorTypeLastSuperAdmin       = "/problems/last-super-admin"
	ErrorTypeRetiredEndpoint      = "/problems/retired-endpoint"
	ErrorTypeOrgSuspended         = "/problems/org-suspended"
)

// ErrNotFound creates a not found error.
func ErrNotFound(detail string) *HTTPError {
	return &HTTPError{
		Status: http.StatusNotFound,
		Type:   ErrorTypeNotFound,
		Title:  "Not Found",
		Detail: detail,
	}
}

// ErrInvalidRequest creates a bad request error.
func ErrInvalidRequest(detail string) *HTTPError {
	return &HTTPError{
		Status: http.StatusBadRequest,
		Type:   ErrorTypeInvalidRequest,
		Title:  "Invalid Request",
		Detail: detail,
	}
}

// ErrUnauthorized creates an unauthorized error.
func ErrUnauthorized(detail string) *HTTPError {
	return &HTTPError{
		Status: http.StatusUnauthorized,
		Type:   ErrorTypeUnauthorized,
		Title:  "Unauthorized",
		Detail: detail,
	}
}

// ErrForbidden creates a forbidden error.
func ErrForbidden(detail string) *HTTPError {
	return &HTTPError{
		Status: http.StatusForbidden,
		Type:   ErrorTypeForbidden,
		Title:  "Forbidden",
		Detail: detail,
	}
}

// ErrConflict creates a conflict error.
func ErrConflict(detail string) *HTTPError {
	return &HTTPError{
		Status: http.StatusConflict,
		Type:   ErrorTypeConflict,
		Title:  "Conflict",
		Detail: detail,
	}
}

// ErrInternalServer creates an internal server error.
func ErrInternalServer(detail string) *HTTPError {
	return &HTTPError{
		Status: http.StatusInternalServerError,
		Type:   ErrorTypeInternalError,
		Title:  "Internal Server Error",
		Detail: detail,
	}
}

// WriteError writes an HTTP error response using RFC 9457 format.
func WriteError(w http.ResponseWriter, r *http.Request, err *HTTPError) {
	rfc9457.NewRFC9457(
		rfc9457.WithStatus(err.Status),
		rfc9457.WithType(err.Type),
		rfc9457.WithTitle(err.Title),
		rfc9457.WithDetail(err.Detail),
		rfc9457.WithInstance(r.URL.Path),
	).ServeHTTP(w, r)
}

// MapDomainError maps a domain error to an HTTPError.
func MapDomainError(err error) *HTTPError {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return ErrNotFound("The requested resource was not found")
	case errors.Is(err, domain.ErrAlreadyExists):
		return ErrConflict("A resource with the same identifier already exists")
	case errors.Is(err, domain.ErrInvalidStatus):
		return ErrInvalidRequest("Invalid status transition")
	case errors.Is(err, domain.ErrStreamKeyInUse):
		return &HTTPError{
			Status: http.StatusConflict,
			Type:   ErrorTypeStreamKeyInUse,
			Title:  "Stream Key In Use",
			Detail: "This stream key is already in use by an active stream",
		}
	case errors.Is(err, domain.ErrStreamKeyRevoked):
		return &HTTPError{
			Status: http.StatusUnauthorized,
			Type:   ErrorTypeStreamKeyRevoked,
			Title:  "Stream Key Revoked",
			Detail: "This stream key has been revoked",
		}
	case errors.Is(err, domain.ErrStreamKeyExpired):
		return &HTTPError{
			Status: http.StatusUnauthorized,
			Type:   ErrorTypeStreamKeyExpired,
			Title:  "Stream Key Expired",
			Detail: "This stream key has expired",
		}
	case errors.Is(err, domain.ErrInvalidStreamKey):
		return &HTTPError{
			Status: http.StatusUnauthorized,
			Type:   ErrorTypeInvalidStreamKey,
			Title:  "Invalid Stream Key",
			Detail: "The provided stream key is invalid",
		}
	case errors.Is(err, domain.ErrUnauthorized):
		return ErrUnauthorized("Unauthorized")
	case errors.Is(err, domain.ErrAdminRequired):
		return ErrForbidden("Admin privileges required")
	case errors.Is(err, domain.ErrForbidden):
		return ErrForbidden("Access denied")

	// --- 003-multi-tenant-platform mappings ---
	case errors.Is(err, domain.ErrNoOrgMembership):
		return &HTTPError{
			Status: http.StatusForbidden,
			Type:   ErrorTypeNoOrgMembership,
			Title:  "No Organization Membership",
			Detail: "Your account is not associated with any organization yet",
		}
	case errors.Is(err, domain.ErrNotInOrg):
		return &HTTPError{
			Status: http.StatusForbidden,
			Type:   ErrorTypeNotInOrg,
			Title:  "Resource Belongs To A Different Organization",
			Detail: "You do not have access to resources outside your organization",
		}
	case errors.Is(err, domain.ErrACLDenied):
		return &HTTPError{
			Status: http.StatusForbidden,
			Type:   ErrorTypeACLDenied,
			Title:  "Access Denied",
			Detail: "Your attributes do not satisfy this room's access control rules",
		}
	case errors.Is(err, domain.ErrRoomArchived):
		return &HTTPError{
			Status: http.StatusConflict,
			Type:   ErrorTypeRoomArchived,
			Title:  "Room Archived",
			Detail: "This room is archived and rejects new streams or mutations",
		}
	case errors.Is(err, domain.ErrStaleRoomVersion):
		return &HTTPError{
			Status: http.StatusConflict,
			Type:   ErrorTypeStaleRoomVersion,
			Title:  "Stale Room Version",
			Detail: "The room was modified by someone else; refresh and retry",
		}
	case errors.Is(err, domain.ErrDeviceKeyRevoked):
		return &HTTPError{
			Status: http.StatusUnauthorized,
			Type:   ErrorTypeDeviceKeyRevoked,
			Title:  "Device Key Revoked",
			Detail: "The provided device key has been revoked",
		}
	case errors.Is(err, domain.ErrSessionInvalidated):
		return &HTTPError{
			Status: http.StatusUnauthorized,
			Type:   ErrorTypeSessionInvalidated,
			Title:  "Session Invalidated",
			Detail: "Your session has been invalidated; please sign in again",
		}
	case errors.Is(err, domain.ErrWorkspaceDomainTaken):
		return &HTTPError{
			Status: http.StatusConflict,
			Type:   ErrorTypeWorkspaceDomainTaken,
			Title:  "Workspace Domain Already Taken",
			Detail: "Another team has already claimed this workspace domain",
		}
	case errors.Is(err, domain.ErrLastSuperAdmin):
		return &HTTPError{
			Status: http.StatusConflict,
			Type:   ErrorTypeLastSuperAdmin,
			Title:  "Cannot Remove The Last Super-Admin",
			Detail: "At least one super-admin must remain on the platform at all times",
		}
	case errors.Is(err, domain.ErrRetiredEndpoint):
		return &HTTPError{
			Status: http.StatusGone,
			Type:   ErrorTypeRetiredEndpoint,
			Title:  "Endpoint Retired",
			Detail: "This endpoint has been retired in favor of a v2 replacement",
		}
	case errors.Is(err, domain.ErrOrgSuspended):
		return &HTTPError{
			Status: http.StatusForbidden,
			Type:   ErrorTypeOrgSuspended,
			Title:  "Organization Suspended",
			Detail: "This organization is currently suspended; contact a platform administrator",
		}

	default:
		return ErrInternalServer("An unexpected error occurred")
	}
}
