package fuzz

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/fd0/termstatus"
)

// Terminal prints data with intermediate status.
type Terminal interface {
	Printf(msg string, data ...interface{})
	Print(msg string)
	SetStatus([]string)
	Run(context.Context)
}

// LogTerminal writes data to a second writer in addition to the terminal.
type LogTerminal struct {
	*termstatus.Terminal
	w io.WriteCloser
}

// Printf prints a messsage with formatting.
func (lt *LogTerminal) Printf(msg string, data ...interface{}) {
	lt.Print(fmt.Sprintf(msg, data...))
}

// Print prints a message.
func (lt *LogTerminal) Print(msg string) {
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	lt.Terminal.Print(msg)
	fmt.Fprintf(lt.w, msg)
}

// Reporter prints the Responses to stdout.
type Reporter struct {
	term    Terminal
	filters []ResponseFilter
}

// NewReporter returns a new reporter.
func NewReporter(term Terminal, filters []ResponseFilter) *Reporter {
	return &Reporter{term: term, filters: filters}
}

// HTTPStats collects statistics about several HTTP responses.
type HTTPStats struct {
	Start       time.Time
	StatusCodes map[int]int
	Errors      int
	Responses   int
	Count       int

	lastRPS time.Time
	rps     float64
}

func formatSeconds(secs float64) string {
	sec := int(secs)
	hours := sec / 3600
	sec -= hours * 3600
	min := sec / 60
	sec -= min * 60
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", hours, min, sec)
	}

	return fmt.Sprintf("%dm%02ds", min, sec)
}

// Report returns a report about the received HTTP status codes.
func (h *HTTPStats) Report(current string) (res []string) {
	res = append(res, "")
	status := fmt.Sprintf("%v requests", h.Responses)
	dur := time.Since(h.Start) / time.Second
	if dur > 0 && time.Since(h.lastRPS) > time.Second {
		h.rps = float64(h.Responses) / float64(dur)
		h.lastRPS = time.Now()
	}
	if h.rps > 0 {
		status += fmt.Sprintf(", %.0f req/s", h.rps)
	}

	if h.Count > 0 {
		todo := h.Count - h.Responses
		status += fmt.Sprintf(", %d todo", todo)

		if h.rps > 0 {
			rem := float64(todo) / h.rps
			status += fmt.Sprintf(", %s remaining", formatSeconds(rem))
		}
	}

	if current != "" {
		status += fmt.Sprintf(", current: %v", current)
	}

	res = append(res, status)

	for code, count := range h.StatusCodes {
		res = append(res, fmt.Sprintf("%v: %v", code, count))
	}
	sort.Sort(sort.StringSlice(res[2:]))

	return res
}

// Display shows incoming Responses.
func (r *Reporter) Display(ch <-chan Response, countChannel <-chan int) func() error {
	return func() error {
		r.term.Printf("%7s %8s %8s   %-8s %s\n", "status", "header", "body", "value", "extract")

		stats := &HTTPStats{
			Start:       time.Now(),
			StatusCodes: make(map[int]int),
		}

		for response := range ch {
			select {
			case c := <-countChannel:
				stats.Count = c
			default:
			}

			stats.Responses++
			if response.Error != nil {
				stats.Errors++
			} else {
				stats.StatusCodes[response.HTTPResponse.StatusCode]++
			}

			print := true
			for _, f := range r.filters {
				if f.Reject(response) {
					print = false
					break
				}
			}

			if print {
				r.term.Printf("%v\n", response)
			}

			r.term.SetStatus(stats.Report(response.Item))
		}

		r.term.Print("\n")
		r.term.Printf("processed %d HTTP requests in %v\n", stats.Responses, formatSeconds(time.Since(stats.Start).Seconds()))
		for _, line := range stats.Report("")[1:] {
			r.term.Print(line)
		}

		return nil
	}
}