package controllers

import (
	"db-queries/db"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"strings"

	"github.com/lib/pq"
)

const PASSWORD_MIN_LENGTH = 8

type UserDetails struct {
	IsStaff     bool   `json:"isStaff"`
	IsSuperuser bool   `json:"isSuperuser"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	Username    string `json:"username"`
}

func (h *BaseHandler) UsersListAllOrCreateOne(w http.ResponseWriter, req *http.Request) {
	switch {
	case req.Method == "GET":
		handler := JWTMiddleWare(h.GetAllUsers)
		handler.ServeHTTP(w, req)
	case req.Method == "POST":
		h.CreateUser(w, req)
	default:
		http.Error(w, "Method Not Allowed.", http.StatusMethodNotAllowed)
	}
}

func (h *BaseHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var user UserDetails
	err := json.NewDecoder(r.Body).Decode(&user)
	if err != nil {
		http.Error(w, "Invalid payload.", http.StatusBadRequest)
		return
	}

	_, emailParseError := mail.ParseAddress(user.Email)
	if emailParseError != nil || user.Password == "" || user.Username == "" {
		http.Error(w, "Username, password and valid email address required.", http.StatusBadRequest)
		return
	}

	if len(user.Password) < PASSWORD_MIN_LENGTH {
		http.Error(w, fmt.Sprintf("Password min length is %d", PASSWORD_MIN_LENGTH), http.StatusBadRequest)
		return
	}

	staffStatus := false
	if user.IsStaff {
		reqToken := r.Header.Get("Authorization")
		if reqToken == "" {
			http.Error(w, "Token missing.", http.StatusUnauthorized)
			return
		}

		splitToken := strings.Split(reqToken, "Bearer")
		if len(splitToken) != 2 {
			http.Error(w, "Token of wrong format.", http.StatusUnauthorized)
			return
		}

		token := strings.TrimSpace(splitToken[1])
		if token != os.Getenv("STAFF_TOKEN") {
			http.Error(w, "Token invalid", http.StatusUnauthorized)
			return
		}

		staffStatus = true
	}

	if err := db.CreateUser(h.Conn, user.Email, user.Password, user.Username, staffStatus, false); err != nil {
		pqErr := err.(*pq.Error)
		if pqErr.Code.Name() == db.UNIQUE_VIOLATION_ERR_CODE_NAME {
			http.Error(w, "User with specified email already exists.", http.StatusBadRequest)
			return
		}
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *BaseHandler) GetAllUsers(w http.ResponseWriter, authReq *AuthenticatedRequest) {
	if authReq.user.IsStaff || authReq.user.IsSuperuser {
		users, err := db.GetAllUsers(h.Conn)
		if err != nil {
			http.Error(w, "Please try again later.", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(users)
		return
	}
	http.Error(w, "No permissions to perform this action.", http.StatusUnauthorized)
}
