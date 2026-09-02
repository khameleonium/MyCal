package app

import (
	"fmt"

	"mycalendar/color"
)

// checkIntegrity runs a structural check on startup ("Data Doctor"): missing or
// malformed IDs, unparseable dates in IDs, duplicate IDs, negative durations,
// unrecognised times. It offers to repair what can be safely fixed.
func (a *App) checkIntegrity() {
	issues := a.svc.Validate()
	if len(issues) == 0 {
		return
	}
	fmt.Println(color.Yellow(warnMark + fmt.Sprintf(" Data Doctor: обнаружено проблем — %d:", len(issues))))
	shown := issues
	if len(shown) > 10 {
		shown = shown[:10]
	}
	for _, is := range shown {
		id := is.ID
		if id == "" {
			id = "—"
		}
		fmt.Printf("   • [%s] %s\n", id, is.Detail)
	}
	if len(issues) > len(shown) {
		fmt.Printf("   … и ещё %d\n", len(issues)-len(shown))
	}
	fmt.Println(color.Yellow("  Починка: удалит записи с непригодным ID, перегенерирует дубли ID,"))
	fmt.Println(color.Yellow("  обнулит отрицательную длительность, нормализует время."))
	if !a.confirm("Выполнить починку?") {
		return
	}
	changed := a.svc.Repair()
	if err := a.save(); err != nil {
		return
	}
	fmt.Println(color.Green(okMark + fmt.Sprintf(" Исправлено/удалено записей: %d", changed)))
}
