package parse

import "testing"

func TestParseEventDetail(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
<div itemscope itemtype="http://schema.org/Event">
	<h1 itemprop="name">Auchan Live | Academia Maia</h1>
	<p itemprop="location">Academia Auchan Live | Loja da Maia</p>
	<img itemprop="image" src="https://info.ticketline.pt/images/Espectaculos/98164/cartaz.jpg" />
	<span itemprop="startDate" content="2026-08-22">August 22, 2026</span>
	<a itemprop="url" href="/evento/auchan-live-academia-maia-98164">Event Link</a>
	<div class="metadata categories">Formação</div>
</div>
</body>
</html>`

	evt, err := ParseEventDetail(html, 98164)
	if err != nil {
		t.Fatalf("ParseEventDetail failed: %v", err)
	}

	if evt.EventID != 98164 {
		t.Errorf("expected event ID 98164, got %d", evt.EventID)
	}
	if evt.Title != "Auchan Live | Academia Maia" {
		t.Errorf("expected title, got '%s'", evt.Title)
	}
	if evt.Venue != "Academia Auchan Live | Loja da Maia" {
		t.Errorf("expected venue, got '%s'", evt.Venue)
	}
	if evt.ImageURL != "https://info.ticketline.pt/images/Espectaculos/98164/cartaz.jpg" {
		t.Errorf("expected image URL, got '%s'", evt.ImageURL)
	}
}

// TestParseEventDetailNestedPlaceItem reproduces the real ticketline.pt
// session markup on individual /evento/{slug}/sessao/{id} detail pages: the
// event's own "name" lives in a content= attribute (not text), and its
// "location" is a nested schema.org/Place item with its own "name" (venue)
// and "address" itemprops. Both of these previously broke extraction: the
// Event's name was never captured (no content= fallback), and the nested
// Place's "name" silently overwrote it in the flat itemprop map, so every
// session ended up titled with its own venue name instead of its title.
func TestParseEventDetailNestedPlaceItem(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
<li class="" itemprop="Event" itemscope itemtype="http://schema.org/Event">
	<a href="/evento/petiscos-de-verao-a-italiana-106315/sessao/127746_3515_1787394600" itemprop="url">
		<div class="date" itemprop="startDate" content="2026-08-22T11:30">
			<p class="time">11:30</p>
		</div>
		<div class="details" itemprop="name" content="Petiscos De Verão À Italiana">
			<div itemprop="location" itemscope itemtype="http://schema.org/Place">
				<p class="venue" itemprop="name">Academia Auchan Live | Loja Da Maia</p>
				<p class="location" itemprop="address" itemscope itemtype="http://schema.org/PostalAddress">
					<span class="city" itemprop="addressLocality"></span>
					<span class="district" itemprop="addressRegion">Porto</span>
				</p>
			</div>
		</div>
	</a>
</li>
</body>
</html>`

	evt, err := ParseEventDetail(html, 106315)
	if err != nil {
		t.Fatalf("ParseEventDetail failed: %v", err)
	}

	if evt.Title != "Petiscos De Verão À Italiana" {
		t.Errorf("expected event's own name as title, got %q (nested Place.name likely leaked in)", evt.Title)
	}
	if evt.Venue != "Academia Auchan Live | Loja Da Maia" {
		t.Errorf("expected clean venue name, got %q", evt.Venue)
	}
	if evt.EventDate != "2026-08-22T11:30" {
		t.Errorf("expected event date with time, got %q", evt.EventDate)
	}
}

// TestParseEventDetailCategoryFromTagsList reproduces the real detail page
// layout: the event's own category tag lives in a "tags_list" block near
// the top of the page (outside any schema.org/Event microdata), while a
// "similar events" widget further down the same page reuses the
// ".metadata.categories" class for *other*, unrelated events. Category
// extraction must prefer the scoped tags_list, not the first
// ".metadata.categories" match in document order (which belongs to an
// unrelated recommended event).
func TestParseEventDetailCategoryFromTagsList(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
<article class="content event_detail">
	<div class="metadata">
		<h2 class="title">Petiscos de Verão à Italiana</h2>
		<ul class="tags_list clearfix">
			<li><a href="/pesquisa?category=398">FORMAÇÃO</a></li>
		</ul>
	</div>
</article>
<li itemscope itemtype="http://schema.org/Event">
	<a itemprop="url" href="/evento/petiscos-de-verao-a-italiana-106315"></a>
	<span itemprop="name">Petiscos De Verão À Italiana</span>
</li>
<div class="similar_events">
	<li itemscope itemtype="http://schema.org/Event">
		<p class="title" itemprop="name">Some Unrelated Concert</p>
		<p class="metadata categories">Música</p>
	</li>
</div>
</body>
</html>`

	evt, err := ParseEventDetail(html, 106315)
	if err != nil {
		t.Fatalf("ParseEventDetail failed: %v", err)
	}

	if evt.Category != "FORMAÇÃO" {
		t.Errorf("expected this event's own category 'FORMAÇÃO', got %q (likely picked up the unrelated recommended event's category)", evt.Category)
	}
}

// TestParseEventDetailPosterFromThumbAnchor reproduces the real detail page
// layout: the event's own Sessões microdata block carries no itemprop=
// "image" at all, so the poster must come from the plain, non-lazy-loaded
// <a class="thumb"> in the page's header. A "similar events" widget further
// down the same page reuses the "thumb" class on unrelated <div> wrappers
// around lazy-loaded card images — those must not be picked up instead.
func TestParseEventDetailPosterFromThumbAnchor(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
<article class="content event_detail">
	<header class="clearfix">
		<a href="https://info.ticketline.pt/images/Espectaculos/106315/cartaz.jpg?rev=20260818170200" rel="lightbox[event]" class="thumb" title=""><img src="https://info.ticketline.pt/images/Espectaculos/106315/cartaz.jpg?rev=20260818170200" alt="" /></a>
		<div class="metadata">
			<h2 class="title">Petiscos de Verão à Italiana</h2>
		</div>
	</header>
</article>
<li itemscope itemtype="http://schema.org/Event">
	<a itemprop="url" href="/evento/petiscos-de-verao-a-italiana-106315"></a>
	<span itemprop="name">Petiscos De Verão À Italiana</span>
</li>
<div class="similar_events">
	<li itemscope itemtype="http://schema.org/Event">
		<div class="thumb">
			<img src="/static/img/blank.png" data-src-original="https://info.ticketline.pt/images/Espectaculos/999999/cartaz.jpg" itemprop="image" />
		</div>
		<p class="title" itemprop="name">Some Unrelated Concert</p>
	</li>
</div>
</body>
</html>`

	evt, err := ParseEventDetail(html, 106315)
	if err != nil {
		t.Fatalf("ParseEventDetail failed: %v", err)
	}

	want := "https://info.ticketline.pt/images/Espectaculos/106315/cartaz.jpg?rev=20260818170200"
	if evt.ImageURL != want {
		t.Errorf("ImageURL = %q, want %q (either missed the thumb anchor, or picked up the unrelated event's poster)", evt.ImageURL, want)
	}
}

func TestParseEventDetailMissingFields(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
<div itemscope itemtype="http://schema.org/Event">
	<h1 itemprop="name">Minimal Event</h1>
</div>
</body>
</html>`

	evt, err := ParseEventDetail(html, 999)
	if err != nil {
		t.Fatalf("ParseEventDetail failed: %v", err)
	}

	if evt.EventID != 999 {
		t.Errorf("expected event ID 999, got %d", evt.EventID)
	}
	if evt.Title != "Minimal Event" {
		t.Errorf("expected title, got %s", evt.Title)
	}
	if evt.Venue != "" || evt.ImageURL != "" || evt.EventDate != "" {
		t.Errorf("expected empty optional fields")
	}
}

func TestValidateEventDateValid(t *testing.T) {
	tests := []string{
		"2026-08-22",
		"2025-01-01",
		"2099-12-31",
		"2000-06-15",
		"2026-09-04T18:30", // hub session with a fixed time, no seconds/tz
		"2026-09-12T10:00",
	}

	for _, dateStr := range tests {
		err := validateEventDate(dateStr)
		if err != nil {
			t.Errorf("validateEventDate(%q): expected nil, got %v", dateStr, err)
		}
	}
}

func TestValidateEventDateEmpty(t *testing.T) {
	err := validateEventDate("")
	if err != nil {
		t.Errorf("validateEventDate(empty): expected nil, got %v", err)
	}
}

func TestValidateEventDateMalformed(t *testing.T) {
	tests := []string{
		"2026-8-22",  // missing leading zero on month
		"22/08/2026", // wrong separator
		"08-22-2026", // wrong order
		"not-a-date", // invalid string
		"2026/08/22", // wrong separator
		"22-08-2026", // wrong order
		"2026-13-01", // invalid month
		"2026-08-32", // invalid day
	}

	for _, dateStr := range tests {
		err := validateEventDate(dateStr)
		if err == nil {
			t.Errorf("validateEventDate(%q): expected error, got nil", dateStr)
		}
	}
}
