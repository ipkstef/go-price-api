package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"go-price-api/internal/models"
)

type AuthHandler struct {
	DB        *pgxpool.Pool
	JWTSecret string
	JWTExpiry time.Duration
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	var user models.User
	err = h.DB.QueryRow(c.Request.Context(),
		`INSERT INTO users (username, password_hash) VALUES ($1, $2)
		 RETURNING user_id, username, created_at`,
		req.Username, string(hash),
	).Scan(&user.UserID, &user.Username, &user.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "registration failed"})
		}
		return
	}

	token, expiresAt, err := h.issueToken(user.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue token"})
		return
	}

	c.JSON(http.StatusCreated, models.AuthResponse{Token: token, ExpiresAt: expiresAt})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user models.User
	err := h.DB.QueryRow(c.Request.Context(),
		`SELECT user_id, username, password_hash FROM users WHERE username = $1`,
		req.Username,
	).Scan(&user.UserID, &user.Username, &user.PasswordHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		}
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	token, expiresAt, err := h.issueToken(user.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue token"})
		return
	}

	c.JSON(http.StatusOK, models.AuthResponse{Token: token, ExpiresAt: expiresAt})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	userID := c.GetString("user_id")
	id, err := strconv.ParseInt(userID, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user identity in token"})
		return
	}

	token, expiresAt, err := h.issueToken(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue token"})
		return
	}

	c.JSON(http.StatusOK, models.AuthResponse{Token: token, ExpiresAt: expiresAt})
}

func (h *AuthHandler) issueToken(userID int64) (string, int64, error) {
	expiresAt := time.Now().Add(h.JWTExpiry)
	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(userID, 10),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(h.JWTSecret))
	if err != nil {
		return "", 0, err
	}

	return signed, expiresAt.Unix(), nil
}
