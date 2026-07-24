// Package mindful implements a metronome-paced typing challenge used as a
// friction gate. Characters of a randomly chosen sentence are revealed one at a
// time; the user must type each character as it appears. Falling behind the
// beat, mistyping, or losing terminal focus resets the challenge to a *fresh*
// random sentence, so the challenge can never collapse into muscle memory.
//
// The design goal is presence: unlike a plain delay (which you can start and
// walk away from), this pins you to the keyboard for the full duration.
package mindful

import (
	"math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DefaultSentences is the pool drawn from when Options.Sentences is empty. A mix
// of accountability lines and neutral filler; all ASCII so byte index == rune
// index during matching.
var DefaultSentences = []string{
	"I am removing my own guardrails.",
	"This uninstall will be recorded.",
	"Nothing urgent needs this off.",
	"I can wait until tomorrow.",
	"This gate exists for a reason.",
	"A calm hour makes quiet progress.",
	"Patience makes the hard hour easy.",
	"Steady hands finish the day well.",
	"Small choices add up over time.",
	"The river bends around old stones.",
}

// Options configures a challenge. Zero values fall back to sensible defaults.
type Options struct {
	// Sentences is the pool to draw from; empty means DefaultSentences.
	Sentences []string
	// Lines is how many sentences to chain into one target (the friction tier).
	// Default 1.
	Lines int
	// Interval is the per-character reveal cadence. Default 1s.
	Interval time.Duration
	// Deadline is how long after a character is revealed the user has to type it
	// before the challenge resets. Default 2s. Should exceed Interval so the
	// user may lag a character or two but not walk away.
	Deadline time.Duration
	// Grace is a pause before the first character is revealed, so the opening
	// character does not catch the user off guard. Default 1.2s.
	Grace time.Duration
}

func (o Options) withDefaults() Options {
	if len(o.Sentences) == 0 {
		o.Sentences = DefaultSentences
	}
	if o.Lines < 1 {
		o.Lines = 1
	}
	if o.Interval <= 0 {
		o.Interval = time.Second
	}
	if o.Deadline <= 0 {
		o.Deadline = 2 * time.Second
	}
	if o.Grace <= 0 {
		o.Grace = 1200 * time.Millisecond
	}
	return o
}

// Run presents the challenge in the terminal and blocks until it is passed or
// aborted. It returns passed=true only when the user typed the whole target in
// time; aborting (Esc/Ctrl-C) returns passed=false. err is non-nil only on a
// terminal/IO failure.
func Run(opts Options) (passed bool, err error) {
	m := newModel(opts.withDefaults())
	prog := tea.NewProgram(m, tea.WithAltScreen(), tea.WithReportFocus())
	final, err := prog.Run()
	if err != nil {
		return false, err
	}
	return final.(model).done, nil
}

// tickMsg drives the reveal metronome and the miss-deadline check.
type tickMsg time.Time

type model struct {
	opts Options

	target string    // current sentence(s) to type (ASCII)
	typed  int       // count of correctly typed characters
	reveal int       // count of revealed characters
	start  time.Time // when the current attempt began

	attempts   int
	resetAt    time.Time
	resetWhy   string
	done       bool
	aborted    bool
	width      int
	rng        *rand.Rand
}

func newModel(opts Options) model {
	m := model{
		opts:  opts,
		width: 72,
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	m.newTarget(time.Now())
	m.attempts = 1
	return m
}

// newTarget picks a fresh random target and restarts the attempt clock.
func (m *model) newTarget(now time.Time) {
	parts := make([]string, m.opts.Lines)
	for i := range parts {
		parts[i] = m.opts.Sentences[m.rng.Intn(len(m.opts.Sentences))]
	}
	m.target = strings.Join(parts, " ")
	m.typed = 0
	m.reveal = 0
	m.start = now
}

func (m *model) reset(why string, now time.Time) {
	m.attempts++
	m.resetAt = now
	m.resetWhy = why
	m.newTarget(now)
}

// revealTimeFor returns the scheduled reveal instant of character i.
func (m model) revealTimeFor(i int) time.Time {
	return m.start.Add(m.opts.Grace + time.Duration(i)*m.opts.Interval)
}

func (m *model) recomputeReveal(now time.Time) {
	elapsed := now.Sub(m.start) - m.opts.Grace
	r := 0
	if elapsed >= 0 {
		r = int(elapsed/m.opts.Interval) + 1
	}
	if r > len(m.target) {
		r = len(m.target)
	}
	if r > m.reveal {
		m.reveal = r
	}
}

func (m model) Init() tea.Cmd {
	return m.tickCmd()
}

func (m model) tickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = min(msg.Width-4, 72)
		}
		return m, nil

	case tea.BlurMsg:
		// Terminal lost focus — treat as walking away.
		if !m.done && !m.aborted {
			m.reset("focus left the terminal", time.Now())
		}
		return m, nil

	case tickMsg:
		if m.done || m.aborted {
			return m, tea.Quit
		}
		now := time.Time(msg)
		m.recomputeReveal(now)
		// Miss-deadline: the next expected char has been revealed too long ago.
		if m.typed < m.reveal && now.Sub(m.revealTimeFor(m.typed)) > m.opts.Deadline {
			m.reset("too slow — stay with the beat", now)
		}
		return m, m.tickCmd()

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.aborted = true
			return m, tea.Quit
		case tea.KeyRunes, tea.KeySpace:
			var r rune
			if msg.Type == tea.KeySpace {
				r = ' '
			} else if len(msg.Runes) > 0 {
				r = msg.Runes[0]
			} else {
				return m, nil
			}
			// Only act when a revealed-but-untyped char is waiting; keys pressed
			// while caught up to the metronome are no-ops (you can't get ahead).
			if m.typed < m.reveal && m.typed < len(m.target) {
				if r == rune(m.target[m.typed]) {
					m.typed++
					if m.typed == len(m.target) {
						m.done = true
						return m, tea.Quit
					}
				} else {
					m.reset("wrong key", time.Now())
				}
			}
			return m, nil
		}
	}
	return m, nil
}

var (
	styleDone    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))  // green
	stylePending = lipgloss.NewStyle().Reverse(true).Bold(true)          // current char
	styleShown   = lipgloss.NewStyle().Foreground(lipgloss.Color("252")) // revealed, waiting
	styleHidden  = lipgloss.NewStyle().Foreground(lipgloss.Color("238")) // not yet revealed
	styleReset   = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleHead    = lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true)
)

func (m model) View() string {
	var b strings.Builder

	b.WriteString(styleHead.Render("Mindful gate") + "\n")
	b.WriteString(styleDim.Render("Type each character as it appears. Keep pace. Esc aborts.") + "\n\n")

	// The target, rendered character by character with a soft column wrap.
	col := 0
	for i := 0; i < len(m.target); i++ {
		ch := string(m.target[i])
		var cell string
		switch {
		case i < m.typed:
			cell = styleDone.Render(ch)
		case i == m.typed && i < m.reveal:
			if ch == " " {
				cell = stylePending.Render(" ")
			} else {
				cell = stylePending.Render(ch)
			}
		case i < m.reveal:
			cell = styleShown.Render(ch)
		default:
			cell = styleHidden.Render("·")
		}
		b.WriteString(cell)
		col++
		if col >= m.width && ch == " " {
			b.WriteString("\n")
			col = 0
		}
	}
	b.WriteString("\n\n")

	b.WriteString(styleDim.Render(
		"progress "+itoa(m.typed)+"/"+itoa(len(m.target))+
			"   attempt "+itoa(m.attempts)) + "\n")

	if !m.resetAt.IsZero() && time.Since(m.resetAt) < 2*time.Second {
		b.WriteString("\n" + styleReset.Render("↺ reset: "+m.resetWhy) + "\n")
	}

	return b.String()
}

// itoa avoids pulling strconv into the hot render path for tiny ints.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
