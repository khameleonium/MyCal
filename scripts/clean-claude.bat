<# : batch script
@echo off
setlocal
chcp 65001 >nul
where git >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo [Ошибка] Git не найден в системной переменной PATH! Установите Git для Windows.
    pause
    exit /b 1
)
powershell -NoProfile -ExecutionPolicy Bypass -Command "$content = [System.IO.File]::ReadAllText('%~f0', [System.Text.Encoding]::UTF8); Invoke-Expression $content"
exit /b %ERRORLEVEL%
#>

# ==============================================================================
# clean-claude.bat — Универсальный скрипт для Windows поиска и удаления следов
# Claude / AI из любого Git-репозитория (файлы + история коммитов + кэш GitHub).
# ==============================================================================

[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8

Write-Host "=== Универсальный очиститель упоминаний Claude / AI (Windows) ===" -ForegroundColor Cyan
Write-Host ""

# 1. Поиск корня Git-репозитория
$repoRoot = (git rev-parse --show-toplevel 2>$null)
if (-not $repoRoot) {
    Write-Host "[Ошибка] Текущая папка не является Git-репозиторием." -ForegroundColor Red
    Write-Host "Поместите скрипт в любую папку внутри Git-репозитория и запустите снова."
    Read-Host "Нажмите Enter для выхода..."
    exit 1
}

Set-Location $repoRoot
Write-Host "[*] Репозиторий найден: $repoRoot" -ForegroundColor Blue

$scriptName = "clean-claude"

# 2. Проверка незакоммиченных изменений
git diff-index --quiet HEAD -- 2>$null
if ($LASTEXITCODE -ne 0) {
    Write-Host "`n[Внимание] У вас есть незакоммиченные изменения в рабочей копии." -ForegroundColor Yellow
    $ans = Read-Host "Рекомендуется сделать commit или stash. Продолжить всё равно? [y/N]"
    if ($ans -notmatch "^[yYдД]") {
        Write-Host "Отмена."
        exit 0
    }
}

# ==============================================================================
# ШАГ 1: Поиск упоминаний в файлах проекта
# ==============================================================================
Write-Host "`n--- [1/3] Сканирование файлов проекта ---" -ForegroundColor Cyan

$matches = (git grep -I -i "claude" 2>$null | Where-Object { $_ -notmatch $scriptName })

if ($matches) {
    Write-Host "Найдены упоминания Claude в файлах проекта:" -ForegroundColor Yellow
    $matches | Select-Object -First 30 | ForEach-Object { Write-Host "  $_" }
    if ($matches.Count -gt 30) {
        Write-Host "  ... и ещё $($matches.Count - 30) строк." -ForegroundColor Yellow
    }

    Write-Host "`nВыберите действие для файлов проекта:"
    Write-Host "  1) Удалить строки с упоминанием 'Co-Authored-By: Claude' и 'Claude-Session'"
    Write-Host "  2) Оставить файлы как есть (пропустить)"
    $fileChoice = Read-Host "Ваш выбор [1-2] (по умолч. 1)"
    if ([string]::IsNullOrWhiteSpace($fileChoice)) { $fileChoice = "1" }

    if ($fileChoice -eq "1") {
        $filesToClean = (git grep -I -i -l "claude" 2>$null | Where-Object { $_ -notmatch $scriptName })
        foreach ($f in $filesToClean) {
            if (Test-Path $f) {
                $lines = Get-Content $f -Encoding UTF8
                $cleaned = $lines | Where-Object { 
                    $_ -notmatch '(?i)co-authored-by:.*claude' -and 
                    $_ -notmatch '(?i)claude-session:' 
                }
                [System.IO.File]::WriteAllLines((Convert-Path $f), $cleaned, [System.Text.Encoding]::UTF8)
            }
        }
        Write-Host "[OK] Строки Co-Authored-By и Claude-Session удалены из файлов проекта." -ForegroundColor Green
    }
} else {
    Write-Host "[OK] В файлах проекта упоминаний Claude не обнаружено." -ForegroundColor Green
}

# ==============================================================================
# ШАГ 2: Поиск и очистка истории коммитов Git
# ==============================================================================
Write-Host "`n--- [2/3] Сканирование истории коммитов Git ---" -ForegroundColor Cyan

$commitsWithClaude = (git log --all --grep="Claude" -i --oneline)

if (-not $commitsWithClaude) {
    Write-Host "[OK] В истории коммитов (сообщениях) упоминаний Claude не обнаружено!" -ForegroundColor Green
} else {
    $commitCount = ($commitsWithClaude | Measure-Object).Count
    Write-Host "Найдено коммитов с упоминанием Claude: $commitCount" -ForegroundColor Yellow
    $commitsWithClaude | Select-Object -First 15 | ForEach-Object { Write-Host "  $_" }
    if ($commitCount -gt 15) {
        Write-Host "  ... и ещё $($commitCount - 15) коммитов." -ForegroundColor Yellow
    }

    Write-Host "`nЗапустить перезапись истории для удаления упоминаний?" -ForegroundColor Yellow
    Write-Host "Это удалит строки 'Co-Authored-By: Claude...' и 'Claude-Session:...' из всех коммитов,"
    Write-Host "сохраняя авторов коммитов, даты, деревья файлов и теги."
    $rewriteChoice = Read-Host "Выполнить очистку истории? [y/N]"

    if ($rewriteChoice -match "^[yYдД]") {
        $backupBranch = "backup-before-clean-" + [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
        git branch $backupBranch
        Write-Host "[*] Создана резервная ветка: $backupBranch" -ForegroundColor Blue

        Write-Host "[*] Перезапись истории через git filter-branch..." -ForegroundColor Blue

        $env:FILTER_BRANCH_SQUELCH_WARNING = "1"

        # Запускаем filter-branch. Используем sed из поставки Git for Windows (он всегда есть в usr/bin)
        git filter-branch -f --msg-filter "sed -E '/[Cc]o-[Aa]uthored-[Bb]y:.*[Cc]laude/d; /[Cc]laude-[Ss]ession:/d'" --tag-name-filter cat -- --all --tags

        Write-Host "[*] Очистка временных ссылок и сборка мусора (git gc)..." -ForegroundColor Blue
        
        $originalRefs = (git for-each-ref --format="%(refname)" refs/original/)
        if ($originalRefs) {
            foreach ($ref in $originalRefs) {
                git update-ref -d $ref
            }
        }
        git reflog expire --expire-unreachable=now --all
        git gc --prune=now --aggressive 2>$null

        git branch -D $backupBranch 2>$null

        Write-Host "[OK] История коммитов успешно очищена!" -ForegroundColor Green
        Write-Host "`n[Важно] Чтобы изменения применились на GitHub/сервере, выполните:" -ForegroundColor Yellow
        Write-Host "  git push --force --all" -ForegroundColor White
        Write-Host "  git push --force --tags" -ForegroundColor White
    } else {
        Write-Host "Перезапись истории пропущена."
    }
}

# ==============================================================================
# ШАГ 3: Сброс серверного кэша Contributors на GitHub (опционально)
# ==============================================================================
Write-Host "`n--- [3/3] Проверка кэша GitHub Contributors ---" -ForegroundColor Cyan

$remoteUrl = (git config --get remote.origin.url)
$ghInstalled = (Get-Command gh -ErrorAction SilentlyContinue)

if ($remoteUrl -match "github\.com" -and $ghInstalled) {
    Write-Host "[*] Обнаружен репозиторий GitHub и утилита gh." -ForegroundColor Blue
    $ghChoice = Read-Host "Хотите сбросить серверный кэш Contributors на GitHub (переключить дефолтную ветку туда и обратно)? [y/N]"
    if ($ghChoice -match "^[yYдД]") {
        $defaultBranch = (git symbolic-ref refs/remotes/origin/HEAD 2>$null)
        if ($defaultBranch) {
            $defaultBranch = $defaultBranch -replace "^refs/remotes/origin/", ""
        } else {
            $defaultBranch = "main"
        }

        $otherBranch = (git branch -r | Where-Object { $_ -notmatch "HEAD" -and $_ -notmatch $defaultBranch } | Select-Object -First 1)
        if ($otherBranch) {
            $otherBranch = $otherBranch.Trim() -replace "^origin/", ""
            Write-Host "[*] Переключение default branch: $defaultBranch -> $otherBranch -> $defaultBranch..." -ForegroundColor Blue
            gh repo edit --default-branch $otherBranch 2>$null
            gh repo edit --default-branch $defaultBranch 2>$null
            Write-Host "[OK] Серверный пересчёт Contributors на GitHub запущен!" -ForegroundColor Green
        } else {
            $tempBranch = "_cache_flush_temp"
            git branch $tempBranch
            git push -u origin $tempBranch 2>$null
            gh repo edit --default-branch $tempBranch 2>$null
            gh repo edit --default-branch $defaultBranch 2>$null
            git push origin --delete $tempBranch 2>$null
            git branch -D $tempBranch 2>$null
            Write-Host "[OK] Серверный пересчёт Contributors на GitHub запущен через временную ветку!" -ForegroundColor Green
        }
    }
} else {
    Write-Host "[Инфо] Для сброса кэша Contributors на GitHub после git push --force:" -ForegroundColor Blue
    Write-Host "  В браузере откройте Settings -> General -> Default branch, временно смените ветку и верните обратно."
}

Write-Host "`n=== Завершено успешно! ===" -ForegroundColor Green
Read-Host "Нажмите Enter для завершения..."
