package menu

import (
	"fmt"
	"mycalendar/color"
)

// PrintHelp displays the comprehensive help manual with examples.
func PrintHelp() {
	fmt.Println()
	fmt.Println(color.Yellow("============================================="))
	fmt.Println(color.Yellow("        MyCalendar — Справка и Примеры"))
	fmt.Println(color.Yellow("============================================="))
	
	fmt.Println(color.Green("\n1. Использование из командной строки (CLI)"))
	fmt.Println("Программа поддерживает быстрое выполнение команд без входа в главное меню.")
	fmt.Println("Вы можете передавать неполные даты (например, '2026' или '12.2025').")
	fmt.Println()
	fmt.Println(color.Green("  mycal add [дата]"))
	fmt.Println("    Быстрое добавление записи. Если дата не указана, запросит интерактивно.")
	fmt.Println("    Примеры:")
	fmt.Println("      mycal add")
	fmt.Println("      mycal add 15.12.2025")
	fmt.Println()
	fmt.Println(color.Green("  mycal view [период]"))
	fmt.Println("    Просмотр записей за период.")
	fmt.Println("    Примеры:")
	fmt.Println("      mycal view           — спросит период интерактивно")
	fmt.Println("      mycal view 2026      — все записи за 2026 год")
	fmt.Println("      mycal view 12.2025   — все записи за декабрь 2025")
	fmt.Println("      mycal view 15-20     — записи с 15 по 20 число текущего месяца")
	fmt.Println()
	fmt.Println(color.Green("  mycal delete [период|id]"))
	fmt.Println("    Удаление записей. Можно передать точный ID или период.")
	fmt.Println("    Примеры:")
	fmt.Println("      mycal delete 2026")
	fmt.Println("      mycal delete c4b5e6")
	fmt.Println()
	fmt.Println(color.Green("  mycal export [период]"))
	fmt.Println("    Экспорт записей в CSV.")
	fmt.Println("    Примеры:")
	fmt.Println("      mycal export 2026")
	fmt.Println()
	fmt.Println(color.Green("  mycal today (или t), week (w), month (m)"))
	fmt.Println("    Быстрый просмотр записей за сегодня, эту неделю или этот месяц.")
	
	fmt.Println(color.Green("\n2. Умный ввод дат и времени"))
	fmt.Println("  - При добавлении записи можно указывать только часы: введите '15', программа поймет это как '15:00'.")
	fmt.Println("  - Везде, где требуется период, можно ввести только год (выведутся все записи года), только месяц (текущего года) или диапазон (10-15).")
	
	fmt.Println(color.Green("\n3. Горячие клавиши (Hotkeys)"))
	fmt.Println("  - В главном и вложенных меню вместо цифр можно нажимать буквы!")
	fmt.Println("  - Поддерживается как английская, так и русская раскладка.")
	fmt.Println("  - Например, в меню 'Просмотр записей' можно нажать 'a' (или 'ф'), чтобы выбрать пункт 'Все записи'.")

	fmt.Println(color.Green("\n4. Работа в режиме просмотра записей"))
	fmt.Println("  - Введите порядковый номер или ID (14 цифр), чтобы посмотреть подробности записи.")
	fmt.Println("  - Введите 'all' (или 'все', 'a', 'ф'), чтобы сразу развернуть подробности всех записей в списке.")
	fmt.Println("  - Введите '\\' перед номером или ID (например, '\\3' или '\\20261209110005'), чтобы быстро отредактировать запись.")
	fmt.Println("  - При редактировании повторяющейся записи вы можете изменить либо только её, либо всю серию.")
	fmt.Println("  - Введите '/' и любой текст, чтобы отфильтровать текущий список (например, '/работа').")
	
	fmt.Println(color.Yellow("\n=============================================\n"))
}
