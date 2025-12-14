package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/dto"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/location"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/ports/secondary"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
)

type EventService struct {
	repo secondary.EventRepository
}

func NewEventService(storage secondary.EventRepository) *EventService {
	return &EventService{
		repo: storage,
	}
}

func (s *EventService) Create(ctx context.Context, event *entity.Event) (*entity.Event, error) {
	return s.repo.Create(ctx, event)
}

func (s *EventService) Get(ctx context.Context, id string) (*entity.Event, error) {
	return s.repo.Get(ctx, id)
}

func (s *EventService) GetByQRCodeID(ctx context.Context, qrCodeID string) (*entity.Event, error) {
	return s.repo.GetByQRCodeID(ctx, qrCodeID)
}

func (s *EventService) GetMany(ctx context.Context, ids []string) ([]entity.Event, error) {
	return s.repo.GetMany(ctx, ids)
}

func (s *EventService) GetAll(ctx context.Context) ([]entity.Event, error) {
	return s.repo.GetAll(ctx)
}

func (s *EventService) GetByClubID(ctx context.Context, limit, offset int, clubID string) ([]entity.Event, error) {
	return s.repo.GetByClubID(ctx, limit, offset, clubID)
}

func (s *EventService) CountByClubID(ctx context.Context, clubID string) (int64, error) {
	return s.repo.CountByClubID(ctx, clubID)
}

func (s *EventService) GetFutureByClubID(
	ctx context.Context,
	limit,
	offset int,
	order string,
	clubID string,
	additionalTime time.Duration,
) ([]entity.Event, error) {
	return s.repo.GetFutureByClubID(ctx, limit, offset, order, clubID, additionalTime)
}

// func (s *EventService) CountFutureByClubID(ctx context.Context, clubID string) (int64, error) {
//	return s.repo.CountFutureByClubID(ctx, clubID)
//}

func (s *EventService) Update(ctx context.Context, event *entity.Event) (*entity.Event, error) {
	return s.repo.Update(ctx, event)
}

func (s *EventService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

func (s *EventService) Count(ctx context.Context, role entity.Role) (int64, error) {
	return s.repo.Count(ctx, string(role))
}

func (s *EventService) GetWithPagination(ctx context.Context, limit, offset int, order string, role entity.Role, userID int64) ([]dto.Event, error) {
	return s.repo.GetWithPagination(ctx, limit, offset, order, string(role), userID)
}

// GenerateWeeklyDigestImage generates images of the weekly events digest
// GetWeeklyEvents filters events for the current week (Monday to Sunday).
// Logic: Start of week is the Monday of the current week.
// End of week is the following Monday.
// Events are included if StartTime >= startOfWeek - 1 day and < endOfWeek.
// This ensures the current week including Sunday.
func (s *EventService) GetWeeklyEvents(ctx context.Context) ([]entity.Event, error) {
	now := time.Now().In(location.Location())
	startOfWeek := now.AddDate(0, 0, -int(now.Weekday()-time.Monday))
	if now.Weekday() == time.Sunday {
		startOfWeek = now.AddDate(0, 0, -6)
	}
	endOfWeek := startOfWeek.AddDate(0, 0, 7)

	allEvents, err := s.repo.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	var weeklyEvents []entity.Event
	for _, event := range allEvents {
		if event.StartTime.After(startOfWeek.AddDate(0, 0, -1)) && event.StartTime.Before(endOfWeek) {
			weeklyEvents = append(weeklyEvents, event)
		}
	}
	return weeklyEvents, nil
}

// groupEventsByDay groups events by their start day (truncated to day).
func groupEventsByDay(events []entity.Event) map[time.Time][]entity.Event {
	eventsByDay := make(map[time.Time][]entity.Event)
	for _, event := range events {
		day := event.StartTime.In(location.Location()).Truncate(24 * time.Hour)
		eventsByDay[day] = append(eventsByDay[day], event)
	}
	return eventsByDay
}

// generateWeekDays generates 7 days starting from the given startOfWeek (Monday).
func generateWeekDays(startOfWeek time.Time) []time.Time {
	days := make([]time.Time, 7)
	for i := 0; i < 7; i++ {
		days[i] = startOfWeek.AddDate(0, 0, i)
	}
	return days
}

func (s *EventService) GenerateWeeklyDigestImage(events []entity.Event) ([][]byte, error) {
	// Always use current week starting from Monday
	now := time.Now().In(location.Location())
	startOfWeek := now.AddDate(0, 0, -int(now.Weekday()-time.Monday))
	if now.Weekday() == time.Sunday {
		startOfWeek = now.AddDate(0, 0, -6)
	}
	startOfWeek = startOfWeek.Truncate(24 * time.Hour)

	days := generateWeekDays(startOfWeek)
	eventsByDay := groupEventsByDay(events)

	// Calculate approximate height: base 400px (calendar) + 60px per event, max 2000px
	totalEvents := 0
	for _, evs := range eventsByDay {
		totalEvents += len(evs)
	}
	calculatedHeight := 400 + totalEvents*60
	scale := 1.0
	maxHeight := 2000
	if calculatedHeight > maxHeight {
		scale = float64(maxHeight) / float64(calculatedHeight)
	}
	clipHeight := int(float64(calculatedHeight) * scale)
	if clipHeight > maxHeight {
		clipHeight = maxHeight
	}

	// Generate HTML
	html := generateDigestHTML(days, eventsByDay, scale)

	// Convert HTML to image using export-html
	image, err := htmlToImage(html, clipHeight)
	if err != nil {
		return nil, err
	}
	return [][]byte{image}, nil
}

func generateDigestHTML(days []time.Time, eventsByDay map[time.Time][]entity.Event, scale float64) string {
	// Template based on clubs.html
	tmpl := template.Must(template.New("digest").Parse(`
<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>События недели</title>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        html, body {
            font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #ececec;
            padding: 0;
            width: 640px;
            margin: 0;
            overflow-x: hidden;
        }
        .page { width: 640px; margin: 0; background: #ececec; page-break-after: always; }
        .calendar-page { padding: 40px; background: #ececec; text-align: center; }
        .header { margin-bottom: 32px; }
        .title { font-size: 36px; font-weight: 700; color: #000; margin-bottom: 8px; }
        .month { font-size: 14px; color: #666; text-transform: uppercase; letter-spacing: 1px; }
        .calendar-dates-wrapper { margin-top: 32px; margin-bottom: 32px; }
        .dates-row { display: grid; grid-template-columns: repeat(7, 1fr); gap: 40px; position: relative; padding-bottom: 10px; margin-bottom: 16px; }
        .dates-row::after { content: ""; position: absolute; left: 0; right: 0; bottom: 0; height: 1px; background: #dddddd; }
        .dots-row { display: grid; grid-template-columns: repeat(7, 1fr); gap: 40px; }
        .day-number-cell { text-align: center; font-size: 12px; color: #999; font-weight: 500; }
        .day-number-cell.today { font-weight: 700; color: #000; }
        .day-dots-cell { text-align: center; display: flex; justify-content: center; }
        .day-dots { display: flex; flex-direction: column; gap: 5px; max-height: 55px; flex-wrap: wrap; }
        .dot { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; }
        .dot.empty { background: #c0c0c0; }
        .dot.orange { background: #ff8642; }
        .legend { display: flex; justify-content: center; gap: 16px; flex-wrap: wrap; }
        .legend-item { display: inline-flex; align-items: center; gap: 6px; font-size: 11px; font-weight: 600; padding: 4px 10px; background: #fff; border-radius: 4px; }
        .events-page { padding: 20px 40px 40px 40px; background: #ececec; }
        .events-header { border-bottom: 1px solid #ddd; padding-bottom: 12px; margin-bottom: 12px; }
        .events-container { background: #ececec; }
        .event-item { display: grid; grid-template-columns: 10px 0.5fr 1fr; gap: 12px; margin-bottom: 16px; padding-bottom: 16px; border-bottom: 1px solid #ddd; overflow: hidden; }
        .event-item:last-child { border-bottom: none; margin-bottom: 0; padding-bottom: 0; }
        .event-dot { width: 10px; height: 10px; border-radius: 50%; margin-top: 4px; flex-shrink: 0; background: #ff8642; }
        .event-date { font-size: 12px; font-weight: 600; color: #000; margin-bottom: 3px; }
        .event-time-location { font-size: 11px; color: #666; line-height: 1.4; }
        .event-right { overflow: hidden; }
        .event-title { font-size: 13px; font-weight: 600; color: #000; margin-bottom: 3px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .event-description { font-size: 11px; color: #666; line-height: 1.4; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
        @media print { body { margin: 0; padding: 0; } }
    </style>
</head>
<body style="transform: scale({{.Scale}}); transform-origin: top left;">
<div class="page calendar-page">
    <div class="header">
        <h1 class="title">События недели</h1>
        <p class="month">{{.Month}}</p>
    </div>
    <div class="calendar-dates-wrapper">
        <div class="dates-row">
            {{range .Days}}<div class="day-number-cell{{if .IsToday}} today{{end}}">{{.Day}}</div>{{end}}
        </div>
        <div class="dots-row">
            {{range .Days}}
            <div class="day-dots-cell">
                <div class="day-dots">
                    {{range .Dots}}<div class="dot {{.}}"></div>{{end}}
                </div>
            </div>
            {{end}}
        </div>
    </div>
    <div class="legend">
        <div class="legend-item" style="background: #ff8642;">
            <span style="color: white;">КЛУБЫ</span>
        </div>
    </div>
</div>
<div class="page events-page">
    <div class="events-header">
        <h2 style="font-size: 16px; color: #000;">События недели</h2>
    </div>
    <div class="events-container">
        {{range .Events}}
        <div class="event-item">
            <div class="event-dot"></div>
            <div class="event-left">
                <div class="event-date">{{.Date}}</div>
                <div class="event-time-location">{{.TimeLocation}}</div>
            </div>
            <div class="event-right">
                <div class="event-title">{{.Title}}</div>
                <div class="event-description">{{.Description}}</div>
            </div>
        </div>
        {{end}}
    </div>
</div>
</body>
</html>
`))

	// Prepare data
	type DayData struct {
		Day     int
		Dots    []string
		IsToday bool
	}
	type EventData struct {
		Date         string
		TimeLocation string
		Title        string
		Description  string
	}

	today := time.Now().In(location.Location()).Day()
	var dayDatas []DayData
	for _, day := range days {
		dayNum := day.Day()
		events := eventsByDay[day]
		var dots []string
		for range events {
			dots = append(dots, "orange")
		}
		if len(dots) == 0 {
			dots = []string{"empty"}
		}
		isToday := day.In(location.Location()).Day() == today
		dayDatas = append(dayDatas, DayData{Day: dayNum, Dots: dots, IsToday: isToday})
	}

	var eventDatas []EventData
	for _, day := range days {
		events := eventsByDay[day]
		sort.Slice(events, func(i, j int) bool {
			return events[i].StartTime.Before(events[j].StartTime)
		})
		for _, event := range events {
			dateStr := fmt.Sprintf("%d %s (%s)", day.Day(), getMonthName(day.Month()), getWeekdayName(day.Weekday()))
			var timeStr string
			if event.EndTime.IsZero() {
				timeStr = fmt.Sprintf("%.2d.%.2d | %s", event.StartTime.Hour(), event.StartTime.Minute(), event.Location)
			} else {
				timeStr = fmt.Sprintf("%.2d.%.2d – %.2d.%.2d | %s", event.StartTime.Hour(), event.StartTime.Minute(), event.EndTime.Hour(), event.EndTime.Minute(), event.Location)
			}
			eventDatas = append(eventDatas, EventData{
				Date:         dateStr,
				TimeLocation: timeStr,
				Title:        event.Name,
				Description:  event.Description,
			})
		}
	}

	data := struct {
		Month  string
		Days   []DayData
		Events []EventData
		Scale  float64
	}{
		Month:  fmt.Sprintf("%s %d", getMonthName(days[0].Month()), days[0].Year()),
		Days:   dayDatas,
		Events: eventDatas,
		Scale:  scale,
	}

	var buf bytes.Buffer
	err := tmpl.Execute(&buf, data)
	if err != nil {
		return ""
	}
	return buf.String()
}

func getMonthName(month time.Month) string {
	months := map[time.Month]string{
		time.January:   "январь",
		time.February:  "февраль",
		time.March:     "март",
		time.April:     "апрель",
		time.May:       "май",
		time.June:      "июнь",
		time.July:      "июль",
		time.August:    "август",
		time.September: "сентябрь",
		time.October:   "октябрь",
		time.November:  "ноябрь",
		time.December:  "декабрь",
	}
	return months[month]
}

func getWeekdayName(weekday time.Weekday) string {
	weekdays := map[time.Weekday]string{
		time.Monday:    "понедельник",
		time.Tuesday:   "вторник",
		time.Wednesday: "среда",
		time.Thursday:  "четверг",
		time.Friday:    "пятница",
		time.Saturday:  "суббота",
		time.Sunday:    "воскресенье",
	}
	return weekdays[weekday]
}

func htmlToImage(html string, height int) ([]byte, error) {
	//exportOpts := map[string]interface{}{
	//	"type":     "png",
	//	"fullPage": true,
	//	"viewport": map[string]interface{}{
	//		"width":  640,
	//		"height": 600,
	//	},
	//}
	reqBody := map[string]interface{}{
		"html": html,
		"export": map[string]interface{}{
			"type":     "png",
			"fullPage": false,
			"clip": map[string]interface{}{
				"x":      0,
				"y":      0,
				"width":  640,
				"height": height,
			},
		},
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post("http://export-html:2305/1/screenshot", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("export-html returned status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
