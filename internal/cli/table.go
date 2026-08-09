package cli

import (
	"fmt"
	"io"
	"strings"
)

func printTable(w io.Writer, head []string, rows [][]string) {
	widths := make([]int, len(head))
	for index, value := range head {
		widths[index] = len([]rune(value))
	}
	for _, row := range rows {
		for index, value := range row {
			if width := len([]rune(value)); width > widths[index] {
				widths[index] = width
			}
		}
	}
	line := func(values []string) {
		for index, value := range values {
			if index > 0 {
				fmt.Fprint(w, "  ")
			}
			fmt.Fprint(w, value)
			if index < len(values)-1 {
				fmt.Fprint(w, strings.Repeat(" ", widths[index]-len([]rune(value))))
			}
		}
		fmt.Fprintln(w)
	}
	line(head)
	separators := make([]string, len(widths))
	for index, width := range widths {
		separators[index] = strings.Repeat("─", width)
	}
	line(separators)
	for _, row := range rows {
		line(row)
	}
}
