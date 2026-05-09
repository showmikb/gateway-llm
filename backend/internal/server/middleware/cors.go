package middleware

import (
	"github.com/go-chi/cors"
)

func CORS() cors.Options {
	return cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Requested-With"},
		ExposedHeaders:   []string{"X-Gateway-LLM-Cost", "X-Gateway-LLM-Tokens-Input", "X-Gateway-LLM-Tokens-Output"},
		AllowCredentials: true,
		MaxAge:           300,
	}
}
