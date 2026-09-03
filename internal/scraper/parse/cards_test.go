package parse

import "testing"

func TestParseSearchPageNormalEvent(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
<div class="events">
<li itemscope itemtype="http://schema.org/Event">
	<a itemprop="url" href="/evento/auchan-live-academia-maia-98164">
		<img itemprop="image" src="https://info.ticketline.pt/images/Espectaculos/98164/cartaz.jpg" />
		<h3 itemprop="name">Auchan Live | Academia Maia</h3>
		<span itemprop="location">Academia Auchan Live | Loja da Maia</span>
		<span itemprop="startDate" content="2026-08-22">22 de Agosto de 2026</span>
	</a>
	<div class="metadata">
		<span class="metadata categories">Formação</span>
	</div>
</li>
</div>
</body>
</html>`

	events, err := ParseSearchPage(html)
	if err != nil {
		t.Fatalf("ParseSearchPage failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventID != 98164 {
		t.Errorf("expected event ID 98164, got %d", evt.EventID)
	}
	if evt.Title != "Auchan Live | Academia Maia" {
		t.Errorf("expected title 'Auchan Live | Academia Maia', got '%s'", evt.Title)
	}
	if evt.Venue != "Academia Auchan Live | Loja da Maia" {
		t.Errorf("expected venue, got '%s'", evt.Venue)
	}
	if evt.Category != "Formação" {
		t.Errorf("expected category 'Formação', got '%s'", evt.Category)
	}
	if evt.EventDate != "2026-08-22" {
		t.Errorf("expected event date '2026-08-22', got '%s'", evt.EventDate)
	}
	if evt.ImageURL != "https://info.ticketline.pt/images/Espectaculos/98164/cartaz.jpg" {
		t.Errorf("expected image URL, got '%s'", evt.ImageURL)
	}
	if evt.Slug != "auchan-live-academia-maia-98164" {
		t.Errorf("expected slug 'auchan-live-academia-maia-98164', got '%s'", evt.Slug)
	}
}

// TestParseSearchPageLazyLoadedImage reproduces the real ticketline.pt card
// markup, where itemprop="image" sits on an <img> that's lazy-loaded: its
// src is a generic "/static/img/blank.png" placeholder, and the real poster
// URL only lives in data-src-original. Using src directly (the previous
// behavior) meant every scraped event pointed at the same blank placeholder
// instead of its actual poster.
func TestParseSearchPageLazyLoadedImage(t *testing.T) {
	body := `<!DOCTYPE html>
<html>
<body>
<li itemscope itemtype="http://schema.org/Event">
	<a itemprop="url" href="/evento/petiscos-de-verao-a-italiana-106315">
		<img src="/static/img/blank.png" data-src-original="https://info.ticketline.pt/images/Espectaculos/106315/cartaz.jpg?rev=20260818170200" alt="Petiscos De Verão À Italiana" itemprop="image" />
		<p class="title" itemprop="name">Petiscos de Verão à Italiana</p>
	</a>
</li>
</body>
</html>`

	events, err := ParseSearchPage(body)
	if err != nil {
		t.Fatalf("ParseSearchPage failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	want := "https://info.ticketline.pt/images/Espectaculos/106315/cartaz.jpg?rev=20260818170200"
	if got := events[0].ImageURL; got != want {
		t.Errorf("ImageURL = %q, want %q (likely picked up the blank.png placeholder instead of data-src-original)", got, want)
	}
}

func TestParseSearchPageMissingOptionalFields(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
<li itemscope itemtype="http://schema.org/Event">
	<a itemprop="url" href="/evento/test-event-12345">
		<h3 itemprop="name">Test Event</h3>
	</a>
</li>
</body>
</html>`

	events, err := ParseSearchPage(html)
	if err != nil {
		t.Fatalf("ParseSearchPage failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventID != 12345 {
		t.Errorf("expected event ID 12345, got %d", evt.EventID)
	}
	if evt.Title != "Test Event" {
		t.Errorf("expected title 'Test Event', got '%s'", evt.Title)
	}
	if evt.Venue != "" {
		t.Errorf("expected empty venue, got '%s'", evt.Venue)
	}
	if evt.Category != "" {
		t.Errorf("expected empty category, got '%s'", evt.Category)
	}
	if evt.ImageURL != "" {
		t.Errorf("expected empty image URL, got '%s'", evt.ImageURL)
	}
}

func TestParseSearchPageEmptyPage(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
<div class="no-events">No events found</div>
</body>
</html>`

	events, err := ParseSearchPage(html)
	if err != nil {
		t.Fatalf("ParseSearchPage failed: %v", err)
	}

	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}

func TestParseSearchPageMultipleEvents(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
<li itemscope itemtype="http://schema.org/Event">
	<a itemprop="url" href="/evento/event-one-111">
		<h3 itemprop="name">Event One</h3>
	</a>
</li>
<li itemscope itemtype="http://schema.org/Event">
	<a itemprop="url" href="/evento/event-two-222">
		<h3 itemprop="name">Event Two</h3>
	</a>
</li>
</body>
</html>`

	events, err := ParseSearchPage(html)
	if err != nil {
		t.Fatalf("ParseSearchPage failed: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if events[0].EventID != 111 || events[1].EventID != 222 {
		t.Errorf("unexpected event IDs: %d, %d", events[0].EventID, events[1].EventID)
	}
}

func TestParseSearchPageNestedStructure(t *testing.T) {
	// Test with deeper nesting that matches real-world HTML
	html := `<!DOCTYPE html>
<html>
<body>
<div class="event-list">
	<ul>
		<li itemscope itemtype="http://schema.org/Event">
			<article>
				<div class="event-card">
					<a itemprop="url" href="/evento/deep-nested-event-555">
						<figure>
							<img itemprop="image" src="https://example.com/poster.jpg" alt="Event" />
						</figure>
						<h3><span itemprop="name">Deep Nested Event</span></h3>
						<div>
							<span itemprop="location">Venue Name</span>
						</div>
						<time itemprop="startDate" content="2026-09-15">15 de Setembro</time>
					</a>
					<footer>
						<span class="metadata categories">Festival</span>
					</footer>
				</div>
			</article>
		</li>
	</ul>
</div>
</body>
</html>`

	events, err := ParseSearchPage(html)
	if err != nil {
		t.Fatalf("ParseSearchPage failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventID != 555 {
		t.Errorf("expected event ID 555, got %d", evt.EventID)
	}
	if evt.Title != "Deep Nested Event" {
		t.Errorf("expected title 'Deep Nested Event', got '%s'", evt.Title)
	}
}

func TestParseSearchPageInvalidEventURL(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
<li itemscope itemtype="http://schema.org/Event">
	<a itemprop="url" href="/invalid-url">
		<h3 itemprop="name">Invalid Event</h3>
	</a>
</li>
</body>
</html>`

	events, err := ParseSearchPage(html)
	if err != nil {
		t.Fatalf("ParseSearchPage failed: %v", err)
	}

	// Events with invalid URLs (ID = 0) are skipped
	if len(events) != 0 {
		t.Fatalf("expected 0 events (invalid URL), got %d", len(events))
	}
}
