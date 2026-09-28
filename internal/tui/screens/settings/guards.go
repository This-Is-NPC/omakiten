package settings

import (
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/settingsprojection"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/screenkit"
)

type guardInput struct {
	Snapshot          *config.Snapshot
	Workflow          domain.Workflow
	Label, From, To   string
	Disallowed, Empty string
}

func renderGuardMatrix(kit screenkit.Kit, width int, input guardInput) string {
	rows, widths := guardMatrix(kit, width, input)
	return gridtable.RenderCells(rows, widths, kit.Styles.Border)
}

func guardMatrix(kit screenkit.Kit, width int, input guardInput) (rows [][]gridtable.Cell, widths []int) {
	buckets := settingsprojection.OrderedBuckets(input.Workflow.Buckets)
	transitions := settingsprojection.TransitionIndex(input.Workflow.Transitions)
	rows = make([][]gridtable.Cell, 0, len(buckets)+2)
	// input.Label is the matrix's own title row — the same section-title role
	// Runtime/Project/effective-section tables play in settings.go — so it gets
	// the focused marker. The header and row axis labels below it are column
	// and row headers inside the matrix, not section titles, and stay under
	// kit.Styles.Kicker.
	rows = append(rows, []gridtable.Cell{gridtable.Styled(kit.Styles.FocusKicker(screenkit.Sanitize(input.Label)))})
	header := []gridtable.Cell{gridtable.Styled(kit.Styles.Kicker(screenkit.Sanitize(input.From) + " \\ " + screenkit.Sanitize(input.To)))}
	for _, bucket := range buckets {
		header = append(header, gridtable.Styled(kit.Styles.Kicker(screenkit.Sanitize(bucket.Key))))
	}
	rows = append(rows, header)
	for _, from := range buckets {
		cells := []gridtable.Cell{gridtable.Styled(kit.Styles.Kicker(screenkit.Sanitize(from.Key)))}
		for _, to := range buckets {
			cells = append(cells, gridtable.Raw(guardCell(from, to, transitions, input)))
		}
		rows = append(rows, cells)
	}
	labelWidth, valueWidth := guardColumnWidths(width, buckets, rows)
	widths = []int{labelWidth}
	for range buckets {
		widths = append(widths, valueWidth)
	}
	return rows, widths
}

func guardCell(from, to domain.Bucket, transitions map[[2]string]bool, input guardInput) string {
	if from.Key == to.Key || !transitions[[2]string{from.Key, to.Key}] {
		return screenkit.Sanitize(input.Disallowed)
	}
	if input.Snapshot == nil {
		return screenkit.Sanitize(input.Empty)
	}
	guards := input.Snapshot.Guards(from.ID, to.ID)
	if len(guards) == 0 {
		return input.Empty
	}
	slugs := make([]string, len(guards))
	for i, guard := range guards {
		slugs[i] = screenkit.Sanitize(guard.Type)
	}
	return strings.Join(slugs, ", ")
}

func guardColumnWidths(available int, buckets []domain.Bucket, rows [][]gridtable.Cell) (label, value int) {
	label = 10
	for _, row := range rows {
		if len(row) >= 2 && gridtable.CellWidth(row[0]) > label {
			label = gridtable.CellWidth(row[0])
		}
	}
	value = 12
	if len(buckets) > 0 {
		if width := (available - label - len(buckets) - 1) / len(buckets); width > value {
			value = width
		}
	}
	return label, value
}
