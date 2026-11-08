#!/usr/bin/env bash
# ==============================================================================
# clean-claude.sh — Универсальный скрипт поиска и удаления следов Claude / AI
# из любого Git-репозитория (файлы проекта + история коммитов + кэш GitHub).
# ==============================================================================
set -euo pipefail

# Цвета для вывода в терминал
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

echo -e "${CYAN}${BOLD}=== Универсальный очиститель упоминаний Claude / AI ===${NC}\n"

# 1. Поиск корня Git-репозитория
REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$REPO_ROOT" ]; then
    echo -e "${RED}[Ошибка] Текущая папка не является Git-репозиторием.${NC}"
    echo "Поместите скрипт в любую папку внутри Git-репозитория и запустите снова."
    exit 1
fi

cd "$REPO_ROOT"
echo -e "${BLUE}[*] Репозиторий найден:${NC} $REPO_ROOT"

# Имя скрипта для исключения из поиска
SCRIPT_NAME="$(basename "${BASH_SOURCE[0]}")"

# 2. Проверка незакоммиченных изменений
if ! git diff-index --quiet HEAD -- 2>/dev/null; then
    echo -e "\n${YELLOW}[Внимание] У вас есть незакоммиченные изменения в рабочем каталоге.${NC}"
    read -rp "Рекомендуется сделать stash или commit. Продолжить всё равно? [y/N]: " ans
    if [[ ! "$ans" =~ ^[yYдД] ]]; then
        echo "Отмена."
        exit 0
    fi
fi

# ==============================================================================
# ШАГ 1: Поиск упоминаний в файлах проекта (рабочем каталоге)
# ==============================================================================
echo -e "\n${CYAN}--- [1/3] Сканирование файлов проекта ---${NC}"

# Ищем упоминания claude (регистронезависимо), исключая .git, бинарники, скрипт
MATCHES=$(git grep -I -i "claude" 2>/dev/null | grep -v "$SCRIPT_NAME" || true)

if [ -n "$MATCHES" ]; then
    echo -e "${YELLOW}Найдены упоминания Claude в файлах:${NC}"
    echo "$MATCHES" | head -n 30
    TOTAL_MATCH_COUNT=$(echo "$MATCHES" | wc -l)
    if [ "$TOTAL_MATCH_COUNT" -gt 30 ]; then
        echo -e "${YELLOW}... и ещё $((TOTAL_MATCH_COUNT - 30)) строк.${NC}"
    fi

    echo -e "\nВыберите действие для файлов проекта:"
    echo "  1) Удалить строки с упоминанием 'Co-Authored-By: Claude' и 'Claude-Session'"
    echo "  2) Оставить файлы как есть (пропустить)"
    read -rp "Ваш выбор [1-2] (по умолч. 1): " file_choice
    file_choice="${file_choice:-1}"

    if [ "$file_choice" = "1" ]; then
        # Находим уникальные файлы, содержащие Claude
        FILES_TO_CLEAN=$(git grep -I -i -l "claude" 2>/dev/null | grep -v "$SCRIPT_NAME" || true)
        for f in $FILES_TO_CLEAN; do
            if [ -f "$f" ]; then
                # Удаляем строки Co-Authored-By: Claude и Claude-Session
                sed -i -E '/[Cc]o-[Aa]uthored-[Bb]y:.*[Cc]laude/d' "$f"
                sed -i -E '/[Cc]laude-[Ss]ession:/d' "$f"
            fi
        done
        echo -e "${GREEN}[OK] Строки Co-Authored-By и Claude-Session удалены из файлов проекта.${NC}"
    fi
else
    echo -e "${GREEN}[OK] В файлах проекта упоминаний Claude не обнаружено.${NC}"
fi

# ==============================================================================
# ШАГ 2: Поиск и очистка истории коммитов Git
# ==============================================================================
echo -e "\n${CYAN}--- [2/3] Сканирование истории коммитов Git ---${NC}"

COMMITS_WITH_CLAUDE=$(git log --all --grep="Claude" -i --oneline || true)

if [ -z "$COMMITS_WITH_CLAUDE" ]; then
    echo -e "${GREEN}[OK] В истории коммитов (сообщениях) упоминаний Claude не обнаружено!${NC}"
else
    COMMIT_COUNT=$(echo "$COMMITS_WITH_CLAUDE" | wc -l)
    echo -e "${YELLOW}Найдено коммитов с упоминанием Claude: ${BOLD}${COMMIT_COUNT}${NC}"
    echo "$COMMITS_WITH_CLAUDE" | head -n 15
    if [ "$COMMIT_COUNT" -gt 15 ]; then
        echo -e "${YELLOW}... и ещё $((COMMIT_COUNT - 15)) коммитов.${NC}"
    fi

    echo -e "\n${BOLD}Запустить перезапись истории для удаления упоминаний?${NC}"
    echo "Это удалит строки 'Co-Authored-By: Claude...' и 'Claude-Session:...' из сообщений всех коммитов,"
    echo "сохраняя авторов коммитов, даты, деревья файлов и теги."
    read -rp "Выполнить очистку истории? [y/N]: " rewrite_choice
    if [[ "$rewrite_choice" =~ ^[yYдД] ]]; then
        BACKUP_BRANCH="backup-before-clean-$(date +%s)"
        git branch "$BACKUP_BRANCH"
        echo -e "${BLUE}[*] Создана резервная ветка:${NC} $BACKUP_BRANCH"

        echo -e "${BLUE}[*] Перезапись истории через git filter-branch...${NC}"
        
        # Фильтрация сообщений: удаляем Co-Authored-By с Claude и Claude-Session, а также лишние пустые строки в конце
        FILTER_BRANCH_SQUELCH_WARNING=1 git filter-branch -f --msg-filter '
        python3 -c "
import sys
content = sys.stdin.read()
lines = content.splitlines()
filtered = [
    l for l in lines 
    if not (\"co-authored-by\" in l.lower() and \"claude\" in l.lower()) 
    and not \"claude-session\" in l.lower()
]
while filtered and not filtered[-1].strip():
    filtered.pop()
print(\"\n\".join(filtered))
" 2>/dev/null || awk "
BEGIN { IGNORECASE=1 }
!/co-authored-by.*claude/ && !/claude-session/ { print }
"
        ' --tag-name-filter cat -- --all --tags

        # Удаляем бэкапы filter-branch и очищаем reflog
        echo -e "${BLUE}[*] Удаление временных ссылок и сборка мусора (git gc)...${NC}"
        git for-each-ref --format="%(refname)" refs/original/ | xargs -r -n 1 git update-ref -d
        git reflog expire --expire-unreachable=now --all
        git gc --prune=now --aggressive >/dev/null 2>&1 || true

        # Удаляем локальную ветку бэкапа
        git branch -D "$BACKUP_BRANCH" >/dev/null 2>&1 || true

        echo -e "${GREEN}[OK] История коммитов успешно очищена!${NC}"
        echo -e "${YELLOW}[Важно] Чтобы изменения применились на GitHub/сервере, выполните:${NC}"
        echo -e "  ${BOLD}git push --force --all${NC}"
        echo -e "  ${BOLD}git push --force --tags${NC}"
    else
        echo "Перезапись истории пропущена."
    fi
fi

# ==============================================================================
# ШАГ 3: Сброс серверного кэша Contributors на GitHub (опционально)
# ==============================================================================
echo -e "\n${CYAN}--- [3/3] Проверка кэша GitHub Contributors ---${NC}"

REMOTE_URL="$(git config --get remote.origin.url || true)"
if [[ "$REMOTE_URL" =~ github\.com ]] && command -v gh >/dev/null 2>&1; then
    echo -e "${BLUE}[*] Обнаружен удалённый репозиторий GitHub и утилита gh.${NC}"
    read -rp "Хотите сбросить серверный кэш Contributors на GitHub (переключить дефолтную ветку туда и обратно)? [y/N]: " gh_choice
    if [[ "$gh_choice" =~ ^[yYдД] ]]; then
        DEFAULT_BRANCH="$(git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null | sed 's@^refs/remotes/origin/@@' || echo "main")"
        OTHER_BRANCH="$(git branch -r | grep -v "HEAD" | grep -v "$DEFAULT_BRANCH" | head -n 1 | sed 's@^[[:space:]]*origin/@@' || true)"
        
        if [ -n "$OTHER_BRANCH" ]; then
            echo -e "${BLUE}[*] Переключение default branch: $DEFAULT_BRANCH -> $OTHER_BRANCH -> $DEFAULT_BRANCH...${NC}"
            gh repo edit --default-branch "$OTHER_BRANCH" >/dev/null 2>&1 || true
            gh repo edit --default-branch "$DEFAULT_BRANCH" >/dev/null 2>&1 || true
            echo -e "${GREEN}[OK] Серверный пересчёт Contributors на GitHub запущен!${NC}"
        else
            # Если второй ветки нет, создадим временную, пушнем, переключим и удалим
            TEMP_BRANCH="_cache_flush_temp"
            git branch "$TEMP_BRANCH"
            git push -u origin "$TEMP_BRANCH" >/dev/null 2>&1 || true
            gh repo edit --default-branch "$TEMP_BRANCH" >/dev/null 2>&1 || true
            gh repo edit --default-branch "$DEFAULT_BRANCH" >/dev/null 2>&1 || true
            git push origin --delete "$TEMP_BRANCH" >/dev/null 2>&1 || true
            git branch -D "$TEMP_BRANCH" >/dev/null 2>&1 || true
            echo -e "${GREEN}[OK] Серверный пересчёт Contributors на GitHub запущен через временную ветку!${NC}"
        fi
    fi
else
    echo -e "${BLUE}[Инфо] Для сброса кэша Contributors на GitHub после git push --force:${NC}"
    echo "  Перейдите в Settings -> General -> Default branch, временно смените ветку и верните обратно."
fi

echo -e "\n${GREEN}${BOLD}=== Готово! ===${NC}"
