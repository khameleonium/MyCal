package app

import (
	"fmt"

	"mycalendar/color"
)

// CheckIntegrity runs a lightweight "Data Doctor" on startup: it finds child
// occurrences whose repeating original is missing and offers to remove them.
func (a *App) CheckIntegrity() {
	orphans := a.svc.OrphanRepeats()
	if len(orphans) == 0 {
		return
	}
	fmt.Println(color.Yellow(warnMark + fmt.Sprintf(
		" Data Doctor: обнаружено %d осиротевших повторений (отсутствует оригинал).", len(orphans))))
	if !a.confirm("Удалить их для очистки базы данных?") {
		return
	}
	deleted := 0
	for _, o := range orphans {
		deleted += a.svc.DeleteEntry(o.ID)
	}
	if err := a.svc.Save(a.ctx); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка при сохранении: " + err.Error()))
		return
	}
	fmt.Println(color.Green(okMark + fmt.Sprintf(" Удалено %d записей.", deleted)))
}
