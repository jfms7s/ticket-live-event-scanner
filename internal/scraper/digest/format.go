package digest

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxMessageLen is Telegram's sendMessage limit, in characters.
const MaxMessageLen = 4096

// Format renders d as one Telegram HTML message. Every piece of stored
// text is escaped; the caps in Build keep it well under MaxMessageLen.
func Format(d Digest) string {
	var b strings.Builder

	fmt.Fprintf(&b, "🗓 <b>Ticket scanner · week of %s</b>\n", weekRange(d))

	b.WriteString("\n<b>Next 14 days</b>\n")
	if d.UpcomingTotal == 0 {
		b.WriteString("No sessions in the next 14 days\n")
	}
	for _, s := range d.Upcoming {
		b.WriteString(s.Day.Format("Mon 2 Jan"))
		if s.Time != "" {
			b.WriteString(" " + s.Time)
		}
		b.WriteString(" · " + escape(cut(s.Title)))
		if s.Venue != "" {
			b.WriteString(" · " + escape(cut(s.Venue)))
		}
		if s.Purchased {
			b.WriteString(" ✅")
		}
		b.WriteString("\n")
	}
	if more := d.UpcomingTotal - len(d.Upcoming); more > 0 {
		fmt.Fprintf(&b, "+%d more\n", more)
	}

	if d.NewCount == 0 {
		b.WriteString("\n<b>Nothing new this week</b>\n")
	} else {
		fmt.Fprintf(&b, "\n<b>New this week: %d</b>\n", d.NewCount)
		for _, t := range d.NewTitles {
			b.WriteString("• " + escape(cut(t)) + "\n")
		}
		if more := d.NewCount - len(d.NewTitles); more > 0 {
			fmt.Fprintf(&b, "+%d more\n", more)
		}
	}

	if len(d.Hubs) > 0 {
		b.WriteString("\n<b>Hubs</b>\n")
		for _, h := range d.Hubs {
			b.WriteString(escape(cut(HubName(h.Slug))) + ": ")
			if h.Failed {
				fmt.Fprintf(&b, "last run failed (%d upcoming before)\n", h.Upcoming)
			} else {
				fmt.Fprintf(&b, "%d upcoming\n", h.Upcoming)
			}
		}
	}

	if d.RunsKnown {
		fmt.Fprintf(&b, "\n<b>Runs</b>: %s, %d failed\n", plural(d.Runs, "run"), d.FailedRuns)
	}

	return strings.TrimRight(b.String(), "\n")
}

// weekRange is "27 Oct – 2 Nov", or "3 – 9 Nov" within one month.
func weekRange(d Digest) string {
	if d.WeekStart.Month() == d.WeekEnd.Month() {
		return fmt.Sprintf("%d – %s", d.WeekStart.Day(), d.WeekEnd.Format("2 Jan"))
	}
	return d.WeekStart.Format("2 Jan") + " – " + d.WeekEnd.Format("2 Jan")
}

var trailingID = regexp.MustCompile(`-\d+$`)

// HubName turns a hub slug into a name: "auchan-live-academia-maia-98164"
// is "Auchan Live Academia Maia".
func HubName(slug string) string {
	words := strings.Fields(strings.ReplaceAll(trailingID.ReplaceAllString(slug, ""), "-", " "))
	for i, w := range words {
		r, size := utf8.DecodeRuneInString(w)
		words[i] = string(unicode.ToUpper(r)) + w[size:]
	}
	if len(words) == 0 {
		return slug
	}
	return strings.Join(words, " ")
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// cut shortens s to maxTitleLen characters.
func cut(s string) string {
	if utf8.RuneCountInString(s) <= maxTitleLen {
		return s
	}
	r := []rune(s)
	return string(r[:maxTitleLen-1]) + "…"
}

var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

// escape escapes text for Telegram's HTML parse mode.
func escape(s string) string { return htmlEscaper.Replace(s) }
