package controllers

import (
	"database/sql"
	"db-queries/db"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/lib/pq"
)

const (
	passwordMinLength = 8
	tokenTtlMinutes = 30
)
var jwtKey = []byte(os.Getenv("JWT_KEY"))

type BaseHandler struct {
	Conn *sql.DB
}

func NewBaseHandler(db *sql.DB) *BaseHandler {
	return &BaseHandler{db}
}

type TicketDetails struct {
	Customer string `json: "customer"`
	Topic    string `json: "topic"`
	Contents string `json: "contents"`
}

type UserDetails struct {
	IsStuff     bool   `json: "isStuff"`
	IsSuperuser bool   `json: "isSuperuser"`
	Email       string `json: "email"`
	Password    string `json: "password"`
	Username    string `json: "username"`
}

type Credentials struct {
	Email    	string `json: "email"`
	Password    string `json: "password"`

}
type Claims struct {
	Email string
	jwt.RegisteredClaims
}

func (h *BaseHandler) Pong(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(time.Now().String()))
}

func (h *BaseHandler) TicketsListAllOrCreateOne(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		h.GetAllTickets(w, r)
		return
	}
	if r.Method == "POST" {
		h.CreateTicket(w, r)
		return
	}
	http.Error(w, "Method Not Allowed.", http.StatusMethodNotAllowed)
}

func (h *BaseHandler) CreateTicket(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		http.Error(w, "Payload expected.", http.StatusBadRequest)
		return
	}

	var ticket TicketDetails
	err := json.NewDecoder(r.Body).Decode(&ticket)
	if err != nil {
		http.Error(w, "Invalid payload.", http.StatusBadRequest)
		return
	}

	if ticket.Customer == "" || ticket.Topic == "" || ticket.Contents == "" {
		http.Error(w, "Missing fields in payload", http.StatusBadRequest)
		return
	}

	id, err := db.CreateTicket(h.Conn, ticket.Customer, ticket.Topic, ticket.Contents)
	if err != nil {
		log.Println(err)
		http.Error(w, "Please try again later.", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	resp := make(map[string]int)
	resp["id"] = id
	json.NewEncoder(w).Encode(resp)
}

func (h *BaseHandler) GetAllTickets(w http.ResponseWriter, r *http.Request) {
	tickets, err := db.GetAllTickets(h.Conn)
	if err != nil {
		log.Println(err)
		http.Error(w, "Please try again later.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(tickets.Tickets)
}

func (h *BaseHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed.", http.StatusMethodNotAllowed)
		return
	}
	
	var user UserDetails
	err := json.NewDecoder(r.Body).Decode(&user)
	if err != nil {
		http.Error(w, "Invalid payload.", http.StatusBadRequest)
		return
	}

	_, emailParseError := mail.ParseAddress(user.Email)
	if emailParseError != nil ||  user.Password == "" || user.Username == "" {
		http.Error(w, "Username, password and valid email address required.", http.StatusBadRequest)
		return
	}

	if len(user.Password) < passwordMinLength {
		http.Error(w, fmt.Sprintf("Password min length is %d", passwordMinLength), http.StatusBadRequest)
		return
	}

	staffStatus := false
	if user.IsStuff {
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
		if token != os.Getenv("STUFF_TOKEN") {
			http.Error(w, "Token invalid", http.StatusUnauthorized)
			return
		}

		staffStatus = true
	}

	id, err := db.CreateUser(h.Conn, user.Email, user.Password, user.Username, staffStatus, false)
	if err != nil {
		pqErr := err.(*pq.Error)
		if pqErr.Code.Name() == "unique_violation" {
			http.Error(w, "User with specified email already exists.", http.StatusBadRequest)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	resp := make(map[string]int)
	resp["id"] = id
	json.NewEncoder(w).Encode(resp)
}

func (h *BaseHandler) LogIn(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
	}

	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed.", http.StatusMethodNotAllowed)
		return
	}

	var creds Credentials
	err := json.NewDecoder(r.Body).Decode(&creds)
	if err != nil {
		http.Error(w, "Email and password required to obtain a token.", http.StatusBadRequest)
		return
	}

	_, emailParseError := mail.ParseAddress(creds.Email)
	if emailParseError != nil || creds.Password == "" {
		http.Error(w, "Valid email address and password required.", http.StatusBadRequest)
		return
	}

	userExists, err := db.UserExists(h.Conn, creds.Email, creds.Password)
	if err != nil {
		log.Println(err)
		http.Error(w, "Please try again later.", http.StatusInternalServerError)
		return
	}
	if !userExists {
		http.Error(w, "User with specified credentials not found.", http.StatusNotFound)
		return
	}

	ttl := time.Now().Add(tokenTtlMinutes * time.Minute)
	tokenString, err := createTokenForUser(creds.Email, ttl)
	if err != nil {
		log.Println(err)
		http.Error(w, "Please try again later.", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name: "token",
		Value: tokenString,
		Expires: ttl,
	})
}

func createTokenForUser(email string, ttl time.Time) (etokenString string, err error) {
	claims := &Claims{
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{ 
			ExpiresAt: jwt.NewNumericDate(ttl),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtKey)
}