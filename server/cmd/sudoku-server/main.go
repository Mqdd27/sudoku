// Command sudoku-server runs the Sudoku backend as a long-lived process behind nginx (or in a container).
package main

import "github.com/Mqdd27/sudoku/server"

func main() { server.Main() }
