// Package handler is the Vercel Function for /api: accounts, Story and the leaderboard on Turso.
package handler

import (
	"net/http"

	"github.com/Mqdd27/sudoku/server"
)

func Handler(w http.ResponseWriter, r *http.Request) { server.Handler(w, r) }
