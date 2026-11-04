/**
 * MyCal Web Dashboard Client
 * Zero dependencies, pure native ES6+, full real-time reactivity.
 */

// Application State
const state = {
  sessions: [],
  types: [],
  statuses: ["Активно", "Завершено", "В процессе", "Запланировано", "Пропущено", "Отменено"],
  config: {},
  currentPeriod: "month", // "today", "week", "month", "all", "range", or "day"
  selectedDate: null,    // ISO date string if a single day is selected (YYYY-MM-DD), otherwise null
  rangeStart: null,      // ISO date string (YYYY-MM-DD) for week / month / range
  rangeEnd: null,        // ISO date string (YYYY-MM-DD) for week / month / range
  selectedTypes: new Set(),
  searchQuery: "",
  focusedId: null,
  calYear: new Date().getFullYear(),
  calMonth: new Date().getMonth(), // 0-based (0 = Jan, 10 = Nov)
  mobileTab: "feed",
  statusPopupTargetId: null,
  contextTargetId: null,

  // Pagination state
  pageSize: localStorage.getItem("mycal_page_size") || "all", // 10, 25, 50, 100, or "all"
  currentPage: 1,

  // Custom Range Modal state
  modalRangeStart: null,
  modalRangeEnd: null,
  dualCalYear: new Date().getFullYear(),
  dualCalMonth: new Date().getMonth(),

  // Yearly View Modal state
  yearViewYear: new Date().getFullYear(),
  yearViewSessions: [],

  // Batch Selection & Operations state
  selectedBatchIds: new Set(),
  isBatchStatusPicker: false,
};

// ======================== INITIALIZATION ========================

document.addEventListener("DOMContentLoaded", () => {
  initLiveClock();
  initGlobalKeyboard();
  initOutsideClickDismiss();
  initPaginationUI();
  loadInitialData();
});

function initLiveClock() {
  const clockEl = document.getElementById("liveClock");
  function tick() {
    const now = new Date();
    clockEl.textContent = now.toLocaleTimeString("ru-RU");
  }
  tick();
  setInterval(tick, 1000);
}

function initPaginationUI() {
  const savedSize = localStorage.getItem("mycal_page_size") || "all";
  state.pageSize = savedSize;
  document.querySelectorAll(".size-pill").forEach(p => {
    p.classList.toggle("active", p.dataset.size === String(savedSize));
  });
}

async function loadInitialData() {
  await Promise.all([fetchTypes(), fetchStatuses(), fetchConfig()]);
  setPeriod("month");
}

async function reloadAll() {
  state.searchQuery = "";
  document.getElementById("topSearchInput").value = "";
  setPeriod("month");
}

// ======================== API CLIENT ========================

async function apiFetch(endpoint, options = {}) {
  try {
    const res = await fetch(endpoint, {
      headers: { "Content-Type": "application/json" },
      ...options,
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || `HTTP ${res.status}`);
    }
    return await res.json();
  } catch (err) {
    console.error("API Error:", err);
    throw err;
  }
}

async function fetchTypes() {
  try {
    const data = await apiFetch("/api/types");
    state.types = data.types || [];
    renderTypesFilter();
    updateTypeDatalist();
    updateTypeSelect();
  } catch (e) {
    console.warn("Could not load types:", e);
  }
}

async function fetchStatuses() {
  try {
    const data = await apiFetch("/api/statuses");
    if (data.statuses && data.statuses.length > 0) {
      state.statuses = data.statuses;
      updateStatusSelect();
    }
  } catch (e) {
    console.warn("Could not load statuses:", e);
  }
}

async function fetchConfig() {
  try {
    const data = await apiFetch("/api/config");
    state.config = data.config || {};
    updateNamesDatalist();
    const defaultMode = state.config.default_mode || "cli";
    if (document.getElementById("cfgDefaultMode")) {
      document.getElementById("cfgDefaultMode").value = defaultMode;
    }
    if (document.getElementById("cfgWebPort")) {
      document.getElementById("cfgWebPort").value = state.config.web_port || 8080;
    }
    document.getElementById("cfgDefaultDuration").value = state.config.default_duration || 60;
    document.getElementById("cfgDateCheckMode").value = state.config.date_check_mode || "";
    document.getElementById("cfgAskConflict").value = state.config.ask_on_conflict ? "true" : "false";
    document.getElementById("cfgDataPath").value = state.config.data_path || "~/.mycal";
  } catch (e) {
    console.warn("Could not load config:", e);
  }
}

async function refreshData() {
  const feed = document.getElementById("sessionFeed");
  feed.innerHTML = '<div class="loading-state">Загрузка записей...</div>';

  try {
    const params = new URLSearchParams();
    if (state.currentPeriod === "day" && state.selectedDate) {
      params.set("date", state.selectedDate);
    } else if (state.currentPeriod === "range" && state.rangeStart && state.rangeEnd) {
      params.set("start", state.rangeStart);
      params.set("end", state.rangeEnd);
    } else if (state.currentPeriod === "today") {
      params.set("period", "today");
    } else if (state.currentPeriod === "week") {
      params.set("period", "week");
    } else if (state.currentPeriod === "month") {
      params.set("period", "month");
      const midMonth = new Date(state.calYear, state.calMonth, 15);
      params.set("ref", toISODate(midMonth));
    } else if (state.currentPeriod === "all") {
      params.set("period", "all");
    } else if (state.selectedDate) {
      params.set("date", state.selectedDate);
    } else {
      params.set("period", "all");
    }

    if (state.selectedTypes.size > 0) {
      params.set("type", Array.from(state.selectedTypes).join(","));
    }

    if (state.searchQuery) {
      params.set("q", state.searchQuery);
    }

    const data = await apiFetch(`/api/sessions?${params.toString()}`);
    state.sessions = data.sessions || [];
    updateNamesDatalist();

    renderFeed();
    renderMiniCalendar();
    syncPeriodButtons();
    renderStats(data);

    // Keep focused session active or focus first
    if (state.sessions.length > 0) {
      if (!state.focusedId || !state.sessions.find(s => s.id === state.focusedId)) {
        setFocusedSession(state.sessions[0].id);
      } else {
        setFocusedSession(state.focusedId);
      }
    } else {
      clearInspector();
    }
  } catch (e) {
    feed.innerHTML = `<div class="empty-feed-hint" style="color: var(--accent-red);">Ошибка загрузки: ${e.message}</div>`;
  }
}

// ======================== RENDER FUNCTIONS ========================

function renderFeed() {
  const feed = document.getElementById("sessionFeed");
  const counter = document.getElementById("feedCounter");
  const title = document.getElementById("feedTitle");

  counter.textContent = `${state.sessions.length} записей`;

  if (state.selectedDate) {
    title.textContent = `Записи за ${formatISODate(state.selectedDate)}`;
  } else if (state.currentPeriod === "range" && state.rangeStart && state.rangeEnd) {
    title.textContent = `Записи с ${formatISODate(state.rangeStart)} по ${formatISODate(state.rangeEnd)}`;
  } else {
    switch (state.currentPeriod) {
      case "today": title.textContent = "Записи на сегодня"; break;
      case "week": title.textContent = "Записи за эту неделю"; break;
      case "month": {
        const monthNames = [
          "январь", "февраль", "март", "апрель", "май", "июнь",
          "июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь"
        ];
        title.textContent = `Записи за ${monthNames[state.calMonth]} ${state.calYear} г.`;
        break;
      }
      case "all": title.textContent = "Все записи календаря"; break;
      default: title.textContent = "Список записей";
    }
  }

  const totalItems = state.sessions.length;
  let itemsToRender = state.sessions;
  const pagerEl = document.getElementById("feedPager");
  const pagerInfo = document.getElementById("pagerInfo");
  const pagerPrev = document.getElementById("pagerPrevBtn");
  const pagerNext = document.getElementById("pagerNextBtn");

  if (state.pageSize !== "all" && totalItems > 0) {
    const size = parseInt(state.pageSize, 10);
    const totalPages = Math.max(1, Math.ceil(totalItems / size));
    if (state.currentPage > totalPages) state.currentPage = totalPages;
    if (state.currentPage < 1) state.currentPage = 1;

    const startIdx = (state.currentPage - 1) * size;
    const endIdx = Math.min(startIdx + size, totalItems);
    itemsToRender = state.sessions.slice(startIdx, endIdx);

    if (pagerEl) {
      pagerEl.style.display = "flex";
      pagerInfo.textContent = `Стр. ${state.currentPage} из ${totalPages} (${startIdx + 1}–${endIdx} из ${totalItems})`;
      pagerPrev.disabled = state.currentPage <= 1;
      pagerNext.disabled = state.currentPage >= totalPages;
    }
  } else {
    if (pagerEl) {
      pagerEl.style.display = "none";
    }
  }

  if (state.sessions.length === 0) {
    feed.innerHTML = `
      <div class="empty-feed-hint">
        <div style="font-size: 2rem; margin-bottom: 8px;">📭</div>
        <div>В выбранном периоде записей не найдено.</div>
        <div style="margin-top: 10px;">
          <button type="button" class="btn btn-primary" onclick="openAddModal()">+ Добавить запись</button>
        </div>
      </div>
    `;
    return;
  }

  // Group sessions by Date (YYYY-MM-DD)
  const groups = new Map();
  for (const s of itemsToRender) {
    const d = s.id && s.id.length >= 8 ? `${s.id.slice(0,4)}-${s.id.slice(4,6)}-${s.id.slice(6,8)}` : "Без даты";
    if (!groups.has(d)) {
      groups.set(d, []);
    }
    groups.get(d).push(s);
  }

  let html = "";
  let rowIdx = 1;

  for (const [dateStr, list] of groups.entries()) {
    const dayTotalMin = list.reduce((sum, item) => sum + (item.duration || 0), 0);
    const dayHours = (dayTotalMin / 60).toFixed(1);

    html += `
      <div class="date-header">
        <span>${formatDateHeader(dateStr)}</span>
        <div class="date-header-line"></div>
        <span class="date-header-summary">${list.length} зап. • ${dayHours} ч</span>
      </div>
    `;

    for (const s of list) {
      const isSelected = s.id === state.focusedId;
      const isBatchChecked = state.selectedBatchIds.has(s.id);
      const statusClass = getStatusClass(s.status);
      const statusLabel = s.status ? s.status : "<пусто>";
      const repeatIcon = s.series_id ? '<span class="row-repeat-icon" title="Повторяющаяся серия">↻</span>' : '';
      const isNegative = s.cash < 0;
      const cashBadge = (s.cash !== undefined && s.cash !== 0)
        ? `<span class="row-cash ${isNegative ? 'cash-negative' : ''}" title="Кэш: ${formatCash(s.cash)}">${formatCash(s.cash)}</span>`
        : `<span class="row-cash row-cash-empty" aria-hidden="true"></span>`;

      html += `
        <div class="session-row ${isSelected ? 'selected' : ''} ${isBatchChecked ? 'batch-selected' : ''}" 
             data-id="${s.id}" 
             onclick="onRowClick('${s.id}')"
             ondblclick="onRowDblClick('${s.id}')"
             oncontextmenu="onRowContextMenu(event, '${s.id}')">
          <label class="row-check-label" onclick="event.stopPropagation()">
            <input type="checkbox" class="row-check" data-id="${s.id}" ${isBatchChecked ? 'checked' : ''} onchange="onToggleRowSelect(event, '${s.id}')">
          </label>
          <span class="row-cursor">${isSelected ? '▶' : ''}</span>
          <span class="row-time">${s.time || '--:--'}</span>
          <span class="row-name" title="${escapeHtml(s.name)}">
            ${escapeHtml(s.name)}
            ${repeatIcon}
          </span>
          <span class="row-type" title="${escapeHtml(s.type || '(без типа)')}">
            ${escapeHtml(s.type || '—')}
          </span>
          <span class="row-duration">${s.duration || 0} мин</span>
          ${cashBadge}
          <div class="row-status-wrap">
            <span class="status-pill ${statusClass}" 
                  onclick="onStatusPillClick(event, '${s.id}')" 
                  title="Нажмите, чтобы изменить статус">
              ${escapeHtml(statusLabel)}
            </span>
          </div>
          <button type="button" class="row-more-btn" onclick="onRowContextMenu(event, '${s.id}')" title="Меню действий">⋮</button>
        </div>
      `;
      rowIdx++;
    }
  }

  feed.innerHTML = html;
  updateBatchUI();
}

function renderTypesFilter() {
  const container = document.getElementById("typesFilterList");
  if (!container) return;

  if (state.types.length === 0) {
    container.innerHTML = '<div style="color: var(--text-dim); font-size: 0.76rem; padding: 4px;">Типы не заданы</div>';
    return;
  }

  let html = "";
  for (const t of state.types) {
    const isChecked = state.selectedTypes.size === 0 || state.selectedTypes.has(t);
    const count = state.sessions.filter(s => s.type === t).length;

    html += `
      <div class="type-filter-row ${isChecked ? 'active' : ''}" onclick="toggleTypeFilter('${escapeHtml(t)}')">
        <div class="type-label-left">
          <span class="cb-icon ${isChecked ? '' : 'unchecked'}">${isChecked ? '[✓]' : '[ ]'}</span>
          <span>${escapeHtml(t)}</span>
        </div>
        <span class="type-count-badge">${count}</span>
      </div>
    `;
  }
  container.innerHTML = html;
}

function updateTypeDatalist() {
  const datalist = document.getElementById("typesDatalist");
  if (!datalist) return;
  datalist.innerHTML = state.types.map(t => `<option value="${escapeHtml(t)}">`).join("");
}

function updateTypeSelect(selectedVal) {
  const sel = document.getElementById("formType");
  if (!sel) return;
  const types = state.types || [];
  const curVal = selectedVal !== undefined ? selectedVal : sel.value;
  let optionsHtml = types.map(t => `<option value="${escapeHtml(t)}">${escapeHtml(t)}</option>`).join("");
  if (curVal && !types.includes(curVal) && curVal !== "__custom__") {
    optionsHtml = `<option value="${escapeHtml(curVal)}">${escapeHtml(curVal)}</option>` + optionsHtml;
  }
  optionsHtml += `<option value="__custom__">✏️ + Свой тип...</option>`;
  sel.innerHTML = optionsHtml;
  if (curVal && curVal !== "__custom__") {
    sel.value = curVal;
  } else if (types.length > 0) {
    sel.value = types[0];
  }
}

function promptCustomType() {
  const custom = prompt("Введите новый тип записи:");
  if (custom && custom.trim()) {
    const val = custom.trim();
    if (!state.types.includes(val)) {
      state.types.push(val);
      renderTypesFilter();
      updateTypeDatalist();
    }
    updateTypeSelect(val);
  } else {
    updateTypeSelect();
  }
}

function updateStatusSelect(selectedVal) {
  const sel = document.getElementById("formStatus");
  if (!sel) return;
  const statuses = (state.statuses && state.statuses.length > 0)
    ? state.statuses
    : ["Активно", "Завершено", "В процессе", "Запланировано", "Пропущено", "Отменено"];
  const curVal = selectedVal !== undefined ? selectedVal : sel.value;
  let optionsHtml = statuses.map(st => `<option value="${escapeHtml(st)}">${escapeHtml(st)}</option>`).join("");
  if (curVal && !statuses.includes(curVal) && curVal !== "__custom__") {
    optionsHtml = `<option value="${escapeHtml(curVal)}">${escapeHtml(curVal)}</option>` + optionsHtml;
  }
  optionsHtml += `<option value="__custom__">✏️ + Свой статус...</option>`;
  sel.innerHTML = optionsHtml;
  if (curVal && curVal !== "__custom__") {
    sel.value = curVal;
  } else if (statuses.length > 0) {
    sel.value = statuses[0];
  }
}

function promptCustomStatusInForm() {
  const custom = prompt("Введите новый статус записи (например, 'В ожидании', 'Перенесено'):");
  if (custom && custom.trim()) {
    const val = custom.trim();
    if (!state.statuses.includes(val)) {
      state.statuses.push(val);
    }
    updateStatusSelect(val);
  } else {
    updateStatusSelect();
  }
}

function updateNamesDatalist() {
  const datalist = document.getElementById("namesDatalist");
  if (!datalist) return;
  const seen = new Set();
  const names = [];
  if (state.config && Array.isArray(state.config.custom_names)) {
    for (const n of state.config.custom_names) {
      const s = (n || "").trim();
      if (s && !seen.has(s)) {
        seen.add(s);
        names.push(s);
      }
    }
  }
  const sessionLists = [state.sessions, state.allSessions];
  for (const list of sessionLists) {
    if (Array.isArray(list)) {
      for (const sess of list) {
        const s = (sess.name || "").trim();
        if (s && !seen.has(s)) {
          seen.add(s);
          names.push(s);
        }
      }
    }
  }
  datalist.innerHTML = names.map(n => `<option value="${escapeHtml(n)}">`).join("");
}

function renderMiniCalendar() {
  const title = document.getElementById("calMonthTitle");
  const grid = document.getElementById("calGrid");
  if (!title || !grid) return;

  const monthNames = [
    "Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
    "Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь"
  ];
  title.textContent = `${monthNames[state.calMonth]} ${state.calYear}`;

  grid.className = "cal-grid cal-grid-with-weeks";

  const dayHeaders = ["№", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"];
  let html = dayHeaders.map(d => `<span class="cal-day-header">${d}</span>`).join("");

  const firstDayOfMonth = new Date(state.calYear, state.calMonth, 1);
  const startDayOfWeek = (firstDayOfMonth.getDay() + 6) % 7; // 0 = Mon, 6 = Sun

  const startMonday = new Date(firstDayOfMonth);
  startMonday.setDate(firstDayOfMonth.getDate() - startDayOfWeek);

  const today = new Date();
  const todayISO = toISODate(today);

  // Set of dates having events (check from loaded sessions and year cache)
  const eventDaysSet = new Set();
  for (const s of state.sessions) {
    const d = sessionDate(s);
    if (d) eventDaysSet.add(d);
  }
  if (state.yearViewSessions) {
    for (const s of state.yearViewSessions) {
      const d = sessionDate(s);
      if (d) eventDaysSet.add(d);
    }
  }

  // Render week by week (up to 6 rows)
  for (let w = 0; w < 6; w++) {
    const rowMon = new Date(startMonday);
    rowMon.setDate(startMonday.getDate() + w * 7);

    // Stop if past the month and not the first week
    if (w > 0 && rowMon.getMonth() !== state.calMonth && rowMon > firstDayOfMonth) {
      break;
    }

    const rowSun = new Date(rowMon);
    rowSun.setDate(rowMon.getDate() + 6);

    const rowMonISO = toISODate(rowMon);
    const rowSunISO = toISODate(rowSun);

    const rowThu = new Date(rowMon);
    rowThu.setDate(rowMon.getDate() + 3);
    const weekNum = getISOWeekNumber(rowThu);

    const isRowActive = (state.rangeStart === rowMonISO && state.rangeEnd === rowSunISO);

    // Week button
    html += `<span class="cal-week-btn ${isRowActive ? 'active' : ''}" onclick="selectCalendarWeek('${rowMonISO}', '${rowSunISO}')" title="Выбрать неделю ${weekNum}">Н${weekNum}</span>`;

    // 7 days of this row
    for (let d = 0; d < 7; d++) {
      const cellDate = new Date(rowMon);
      cellDate.setDate(rowMon.getDate() + d);
      const cellISO = toISODate(cellDate);
      const isOtherMonth = cellDate.getMonth() !== state.calMonth;
      const dayNum = cellDate.getDate();

      const isToday = (cellISO === todayISO);
      const hasEvents = eventDaysSet.has(cellISO);

      let isSelected = false;
      let isInRange = false;
      let isStart = false;
      let isEnd = false;

      // NO highlight if "all" period is chosen!
      if (state.currentPeriod !== "all") {
        if (state.currentPeriod === "today" || state.currentPeriod === "day") {
          if (state.selectedDate === cellISO) {
            isSelected = true;
          }
        } else if (state.rangeStart && state.rangeEnd) {
          if (cellISO >= state.rangeStart && cellISO <= state.rangeEnd) {
            isInRange = true;
            if (cellISO === state.rangeStart) isStart = true;
            if (cellISO === state.rangeEnd) isEnd = true;
          }
        }
      }

      const classes = ["cal-day-cell"];
      if (isOtherMonth) classes.push("other-month");
      if (isToday) classes.push("today");
      if (isSelected) classes.push("selected");
      if (isInRange) classes.push("range-selected");
      if (isStart) classes.push("range-start");
      if (isEnd) classes.push("range-end");
      if (hasEvents) classes.push("has-events");

      html += `<span class="${classes.join(' ')}" onclick="selectCalendarDate('${cellISO}')" title="${formatISODate(cellISO)}${hasEvents ? ' (есть записи)' : ''}">${dayNum}</span>`;
    }
  }

  grid.innerHTML = html;
}

function renderStats(data) {
  document.getElementById("statsTotalHours").textContent = `${data.total_hours.toFixed(1)} ч`;
  document.getElementById("statsTotalMin").textContent = `${data.total_minutes} минут`;
  document.getElementById("statsTotalCount").textContent = data.count;

  const avg = data.count > 0 ? (data.total_minutes / data.count).toFixed(1) : 0;
  document.getElementById("statsAvgDur").textContent = `средн. ${avg} мин`;

  const totalCashEl = document.getElementById("statsTotalCash");
  if (totalCashEl) {
    const totalCash = data.total_cash !== undefined ? data.total_cash : 0;
    totalCashEl.textContent = formatCash(totalCash);
  }
  const avgCashEl = document.getElementById("statsAvgCash");
  if (avgCashEl) {
    const totalCash = data.total_cash !== undefined ? data.total_cash : 0;
    const avgCash = data.count > 0 ? (totalCash / data.count) : 0;
    avgCashEl.textContent = `средн. ${formatCash(avgCash)}`;
  }

  // Breakdown by type
  const byType = {};
  for (const s of state.sessions) {
    const t = s.type || "(без типа)";
    byType[t] = (byType[t] || 0) + (s.duration || 0);
  }

  const sortedTypes = Object.entries(byType).sort((a, b) => b[1] - a[1]);
  const barsContainer = document.getElementById("statsBars");

  if (sortedTypes.length === 0) {
    barsContainer.innerHTML = '<div style="color: var(--text-dim); font-size: 0.74rem;">Нет данных</div>';
    return;
  }

  const colors = [
    "var(--accent-blue)", "var(--accent-orange)", "var(--accent-green)",
    "var(--accent-cyan)", "var(--accent-magenta)", "var(--accent-yellow)"
  ];

  let barsHtml = "";
  sortedTypes.forEach(([type, mins], idx) => {
    const pct = data.total_minutes > 0 ? Math.round((mins / data.total_minutes) * 100) : 0;
    const hrs = (mins / 60).toFixed(1);
    const color = colors[idx % colors.length];

    barsHtml += `
      <div class="bar-row">
        <div class="bar-labels">
          <span>${escapeHtml(type)}</span>
          <span style="font-family: var(--font-mono);">${hrs} ч (${pct}%)</span>
        </div>
        <div class="bar-track">
          <div class="bar-fill" style="width: ${pct}%; background: ${color};"></div>
        </div>
      </div>
    `;
  });

  barsContainer.innerHTML = barsHtml;
}

function renderInspector(s) {
  const box = document.getElementById("inspectorContent");
  if (!s) {
    clearInspector();
    return;
  }

  const statusClass = getStatusClass(s.status);
  const statusLabel = s.status ? s.status : "<пусто>";
  const repeatText = s.series_id ? `Серия: ${s.series_id}` : "Одиночная запись";

  box.innerHTML = `
    <div class="detail-field">
      <span class="detail-label">ИД:</span>
      <span class="detail-val" style="font-family: var(--font-mono); color: var(--accent-yellow);">${s.id}</span>
    </div>
    <div class="detail-field">
      <span class="detail-label">Дата:</span>
      <span class="detail-val detail-val-editable" onclick="editFocusedField('date')" title="Нажмите для правки">
        📅 ${formatISODate(sessionDate(s))} ✏️
      </span>
    </div>
    <div class="detail-field">
      <span class="detail-label">Время:</span>
      <span class="detail-val detail-val-editable" onclick="editFocusedField('time')" title="Нажмите для правки">
        🕒 ${s.time} ✏️
      </span>
    </div>
    <div class="detail-field">
      <span class="detail-label">Название:</span>
      <span class="detail-val detail-val-editable" style="color: var(--accent-green); font-weight: 700;" onclick="editFocusedField('name')" title="Нажмите для правки">
        ${escapeHtml(s.name)} ✏️
      </span>
    </div>
    <div class="detail-field">
      <span class="detail-label">Тип:</span>
      <span class="detail-val detail-val-editable" onclick="editFocusedField('type')" title="Нажмите для правки">
        🏷️ ${escapeHtml(s.type || '(без типа)')} ✏️
      </span>
    </div>
    <div class="detail-field">
      <span class="detail-label">Длительность:</span>
      <span class="detail-val detail-val-editable" style="color: var(--accent-orange);" onclick="editFocusedField('duration')" title="Нажмите для правки">
        ⏱️ ${s.duration} мин (${(s.duration / 60).toFixed(1)} ч) ✏️
      </span>
    </div>
    <div class="detail-field">
      <span class="detail-label">Кэш:</span>
      <span class="detail-val detail-val-editable" style="color: var(--accent-yellow); font-family: var(--font-mono);" onclick="editFocusedField('cash')" title="Нажмите для правки">
        ${formatCash(s.cash)} ✏️
      </span>
    </div>
    <div class="detail-field">
      <span class="detail-label">Статус:</span>
      <span class="detail-val">
        <span class="status-pill ${statusClass}" onclick="onStatusPillClick(event, '${s.id}')" title="Нажмите для смены статуса">
          ${escapeHtml(statusLabel)} ▾
        </span>
      </span>
    </div>
    <div class="detail-field">
      <span class="detail-label">Повтор:</span>
      <span class="detail-val" style="color: var(--accent-magenta);">${repeatText}</span>
    </div>
    <div class="detail-field" style="flex-direction: column; gap: 4px; margin-top: 6px;">
      <span class="detail-label">Комментарий:</span>
      <div class="detail-notes-box" onclick="editFocusedField('notes')" title="Нажмите для правки заметки">
        ${s.notes ? escapeHtml(s.notes) : '<span style="color: var(--text-dim); font-style: italic;">Нажмите, чтобы добавить заметку...</span>'}
      </div>
    </div>
  `;
}

function clearInspector() {
  document.getElementById("inspectorContent").innerHTML = `
    <div class="empty-hint">Выберите запись в списке для просмотра деталей</div>
  `;
}

// ======================== INTERACTION HANDLERS ========================

function setFocusedSession(id) {
  state.focusedId = id;
  document.querySelectorAll(".session-row").forEach(r => {
    const isTarget = r.dataset.id === id;
    r.classList.toggle("selected", isTarget);
    const cursor = r.querySelector(".row-cursor");
    if (cursor) cursor.textContent = isTarget ? "▶" : "";
  });

  const s = state.sessions.find(x => x.id === id);
  renderInspector(s);
}

function onRowClick(id) {
  setFocusedSession(id);
}

function onRowDblClick(id) {
  setFocusedSession(id);
  editFocusedSession();
}

function onRowContextMenu(e, id) {
  e.preventDefault();
  e.stopPropagation();
  setFocusedSession(id);
  state.contextTargetId = id;

  const menu = document.getElementById("rowContextMenu");
  menu.style.display = "flex";
  menu.style.left = `${Math.min(e.pageX, window.innerWidth - 200)}px`;
  menu.style.top = `${Math.min(e.pageY, window.innerHeight - 180)}px`;
}

// ----------------- QUICK STATUS PICKER -----------------

function renderStatusPickerPopup() {
  const popup = document.getElementById("statusPickerPopup");
  if (!popup) return;
  const listEl = popup.querySelector(".status-options-list");
  if (!listEl) return;

  const statuses = (state.statuses && state.statuses.length > 0)
    ? state.statuses
    : ["Активно", "Завершено", "В процессе", "Запланировано", "Пропущено", "Отменено"];

  const getStatusMeta = (st) => {
    switch (st) {
      case "Активно": return { dot: "dot-green", icon: "●" };
      case "Завершено": return { dot: "dot-green", icon: "✓" };
      case "В процессе": return { dot: "dot-yellow", icon: "◐" };
      case "Запланировано": return { dot: "dot-purple", icon: "◷" };
      case "Пропущено": return { dot: "dot-orange", icon: "⊘" };
      case "Отменено": return { dot: "dot-red", icon: "✕" };
      default: return { dot: "dot-gray", icon: "•" };
    }
  };

  let html = statuses.map(st => {
    const meta = getStatusMeta(st);
    return `
      <button type="button" class="status-opt-btn" onclick="selectNewStatus('${escapeHtml(st)}')">
        <span class="st-dot ${meta.dot}"></span>
        <span>${escapeHtml(st)}</span>
        <span class="st-icon">${meta.icon}</span>
      </button>
    `;
  }).join("");

  html += `
    <button type="button" class="status-opt-btn status-opt-empty" onclick="selectNewStatus('')">
      <span class="st-dot dot-gray"></span>
      <span>&lt;Очистить статус&gt;</span>
      <span class="st-icon">∅</span>
    </button>
    <button type="button" class="status-opt-btn status-opt-custom" onclick="promptCustomStatus()">
      <span>✏️ Свой статус...</span>
    </button>
  `;

  listEl.innerHTML = html;
}

function onStatusPillClick(e, id) {
  e.preventDefault();
  e.stopPropagation();

  state.statusPopupTargetId = id;
  const pillEl = e.currentTarget;
  const rect = pillEl.getBoundingClientRect();
  const popup = document.getElementById("statusPickerPopup");

  renderStatusPickerPopup();

  popup.style.display = "block";
  popup.style.left = `${Math.min(rect.left + window.scrollX, window.innerWidth - 200)}px`;
  popup.style.top = `${rect.bottom + window.scrollY + 6}px`;

  // Focus popup to enable Esc key handling
  popup.setAttribute("tabindex", "-1");
  popup.focus();
}

async function selectNewStatus(newStatus) {
  if (newStatus && !state.statuses.includes(newStatus)) {
    state.statuses.push(newStatus);
    updateStatusSelect();
  }

  if (state.isBatchStatusPicker) {
    const ids = Array.from(state.selectedBatchIds);
    hideStatusPopup();
    if (ids.length === 0) return;

    try {
      await apiFetch("/api/sessions/batch", {
        method: "PATCH",
        body: JSON.stringify({ ids, field: "status", value: newStatus }),
      });
      state.selectedBatchIds.clear();
      await refreshData();
    } catch (err) {
      alert("Не удалось изменить статус записей: " + err.message);
    }
    return;
  }

  const id = state.statusPopupTargetId;
  hideStatusPopup();
  if (!id) return;

  try {
    const res = await apiFetch(`/api/sessions/${id}/status`, {
      method: "PATCH",
      body: JSON.stringify({ status: newStatus }),
    });

    // Update in local state
    const s = state.sessions.find(x => x.id === id);
    if (s) {
      s.status = newStatus;
    }
    renderFeed();
    if (state.focusedId === id) {
      renderInspector(s);
    }
  } catch (err) {
    alert("Не удалось изменить статус: " + err.message);
  }
}

async function promptCustomStatus() {
  const isBatch = state.isBatchStatusPicker;
  const id = state.statusPopupTargetId;
  hideStatusPopup();
  if (!isBatch && !id) return;

  const custom = prompt("Введите свой статус (например, 'В ожидании', 'Перенесено'):");
  if (custom !== null && custom.trim() !== "") {
    const val = custom.trim();
    if (!state.statuses.includes(val)) {
      state.statuses.push(val);
      updateStatusSelect();
    }
    if (isBatch) {
      state.isBatchStatusPicker = true;
    } else {
      state.statusPopupTargetId = id;
    }
    await selectNewStatus(val);
  }
}

function hideStatusPopup() {
  const popup = document.getElementById("statusPickerPopup");
  if (popup) popup.style.display = "none";
  state.statusPopupTargetId = null;
  state.isBatchStatusPicker = false;
}

function hideContextMenu() {
  const menu = document.getElementById("rowContextMenu");
  if (menu) menu.style.display = "none";
  state.contextTargetId = null;
}

function initOutsideClickDismiss() {
  document.addEventListener("click", (e) => {
    if (!e.target.closest("#statusPickerPopup") && !e.target.closest(".status-pill")) {
      hideStatusPopup();
    }
    if (!e.target.closest("#rowContextMenu") && !e.target.closest(".row-more-btn")) {
      hideContextMenu();
    }
  });

  window.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      hideStatusPopup();
      hideContextMenu();
    }
  });
}

// ----------------- CONTEXT MENU ACTIONS -----------------

function ctxEdit() {
  hideContextMenu();
  editFocusedSession();
}

function ctxChangeStatus() {
  hideContextMenu();
  const row = document.querySelector(`.session-row[data-id="${state.contextTargetId}"]`);
  const pill = row ? row.querySelector(".status-pill") : null;
  if (pill) {
    pill.click();
  }
}

function ctxDuplicate() {
  hideContextMenu();
  duplicateFocusedSession();
}

function ctxDelete() {
  hideContextMenu();
  deleteFocusedSession();
}

// ----------------- CALENDAR NAVIGATION & SYNCHRONIZATION -----------------

function prevMonth() {
  state.calMonth--;
  if (state.calMonth < 0) {
    state.calMonth = 11;
    state.calYear--;
  }
  if (state.currentPeriod === "month") {
    const bounds = getMonthBounds(state.calYear, state.calMonth);
    state.rangeStart = toISODate(bounds.first);
    state.rangeEnd = toISODate(bounds.last);
    refreshData();
  } else {
    renderMiniCalendar();
  }
}

function nextMonth() {
  state.calMonth++;
  if (state.calMonth > 11) {
    state.calMonth = 0;
    state.calYear++;
  }
  if (state.currentPeriod === "month") {
    const bounds = getMonthBounds(state.calYear, state.calMonth);
    state.rangeStart = toISODate(bounds.first);
    state.rangeEnd = toISODate(bounds.last);
    refreshData();
  } else {
    renderMiniCalendar();
  }
}

function goTodayMonth() {
  const now = new Date();
  state.calYear = now.getFullYear();
  state.calMonth = now.getMonth();
  setPeriod("today");
}

function selectCalendarDate(iso) {
  const todayISO = toISODate(new Date());
  state.currentPage = 1;

  if (iso === todayISO) {
    setPeriod("today");
    return;
  }

  // Toggle if already selected single day
  if (state.currentPeriod === "day" && state.selectedDate === iso) {
    setPeriod("month");
    return;
  }

  // Day clicked that is NOT today -> deactivates all top preset buttons!
  state.currentPeriod = "day";
  state.selectedDate = iso;
  state.rangeStart = null;
  state.rangeEnd = null;

  syncPeriodButtons();
  renderMiniCalendar();
  refreshData();
}

function selectCalendarWeek(monISO, sunISO) {
  state.currentPage = 1;
  const curWeek = getWeekBounds(new Date());
  const curMonISO = toISODate(curWeek.monday);
  const curSunISO = toISODate(curWeek.sunday);

  if (monISO === curMonISO && sunISO === curSunISO) {
    setPeriod("week");
  } else {
    state.currentPeriod = "range";
    state.rangeStart = monISO;
    state.rangeEnd = sunISO;
    state.selectedDate = null;
    syncPeriodButtons();
    renderMiniCalendar();
    refreshData();
  }
}

function setPeriod(p, options = {}) {
  state.currentPeriod = p;
  state.currentPage = 1;

  const today = new Date();
  const todayISO = toISODate(today);

  if (p === "today") {
    state.selectedDate = todayISO;
    state.rangeStart = null;
    state.rangeEnd = null;
    state.calYear = today.getFullYear();
    state.calMonth = today.getMonth();
  } else if (p === "week") {
    const bounds = getWeekBounds(today);
    state.selectedDate = null;
    state.rangeStart = toISODate(bounds.monday);
    state.rangeEnd = toISODate(bounds.sunday);
    state.calYear = today.getFullYear();
    state.calMonth = today.getMonth();
  } else if (p === "month") {
    state.selectedDate = null;
    const bounds = getMonthBounds(state.calYear, state.calMonth);
    state.rangeStart = toISODate(bounds.first);
    state.rangeEnd = toISODate(bounds.last);
  } else if (p === "all") {
    // Crucial requirement: No date selected in calendar!
    state.selectedDate = null;
    state.rangeStart = null;
    state.rangeEnd = null;
  } else if (p === "range") {
    if (options.start && options.end) {
      state.rangeStart = options.start;
      state.rangeEnd = options.end;
    }
    state.selectedDate = null;
  }

  syncPeriodButtons();
  renderMiniCalendar();
  refreshData();
}

function syncPeriodButtons() {
  const todayISO = toISODate(new Date());
  const curWeek = getWeekBounds(new Date());
  const curMonISO = toISODate(curWeek.monday);
  const curSunISO = toISODate(curWeek.sunday);

  document.querySelectorAll(".period-tabs .tab-btn").forEach(btn => {
    const p = btn.dataset.period;
    let active = false;

    if (state.currentPeriod === "all" && p === "all") {
      active = true;
    } else if (state.currentPeriod === "today" && p === "today") {
      active = true;
    } else if (state.currentPeriod === "week" && p === "week") {
      active = true;
    } else if (state.currentPeriod === "month" && p === "month") {
      active = true;
    } else if (state.currentPeriod === "range") {
      if (state.rangeStart === curMonISO && state.rangeEnd === curSunISO) {
        if (p === "week") active = true;
      } else {
        if (p === "range") active = true;
      }
    } else if (state.currentPeriod === "day") {
      if (state.selectedDate === todayISO && p === "today") {
        active = true;
      }
      // If clicked day is not today, all buttons are inactive!
    }

    btn.classList.toggle("active", active);
  });

  const rangeBtn = document.querySelector('.tab-btn[data-period="range"]');
  if (rangeBtn) {
    if (state.currentPeriod === "range" && state.rangeStart && state.rangeEnd && (state.rangeStart !== curMonISO || state.rangeEnd !== curSunISO)) {
      rangeBtn.textContent = `Период: ${formatShortDate(state.rangeStart)}–${formatShortDate(state.rangeEnd)}`;
    } else {
      rangeBtn.textContent = "Период";
    }
  }
}

// ----------------- PAGINATION CONTROLS -----------------

function setPageSize(size) {
  state.pageSize = size;
  state.currentPage = 1;
  localStorage.setItem("mycal_page_size", size);
  document.querySelectorAll(".size-pill").forEach(p => {
    p.classList.toggle("active", p.dataset.size == size);
  });
  renderFeed();
}

function changePage(delta) {
  const size = parseInt(state.pageSize, 10);
  if (!size) return;
  const totalPages = Math.ceil(state.sessions.length / size);
  const newPage = state.currentPage + delta;
  if (newPage >= 1 && newPage <= totalPages) {
    state.currentPage = newPage;
    renderFeed();
    const feed = document.getElementById("sessionFeed");
    if (feed) feed.scrollTop = 0;
  }
}

// ----------------- CUSTOM RANGE PICKER MODAL -----------------

function openRangeModal() {
  if (state.rangeStart && state.rangeEnd) {
    state.modalRangeStart = state.rangeStart;
    state.modalRangeEnd = state.rangeEnd;
  } else {
    const today = new Date();
    state.modalRangeStart = toISODate(today);
    state.modalRangeEnd = toISODate(today);
  }

  state.dualCalYear = state.calYear;
  state.dualCalMonth = state.calMonth;

  document.getElementById("rangeStartInput").value = state.modalRangeStart;
  document.getElementById("rangeEndInput").value = state.modalRangeEnd;

  updateRangeInputWeekdays();
  renderDualCalendars();
  openModal("modalRangePicker");
}

function updateRangeInputWeekdays() {
  const sWk = document.getElementById("rangeStartWeekday");
  const eWk = document.getElementById("rangeEndWeekday");
  const summary = document.getElementById("rangeSummaryText");

  if (sWk) sWk.textContent = state.modalRangeStart ? `(${getWeekdayShort(state.modalRangeStart)})` : "";
  if (eWk) eWk.textContent = state.modalRangeEnd ? `(${getWeekdayShort(state.modalRangeEnd)})` : "";

  if (summary) {
    if (state.modalRangeStart && state.modalRangeEnd) {
      const d1 = new Date(state.modalRangeStart + "T00:00:00");
      const d2 = new Date(state.modalRangeEnd + "T00:00:00");
      const diffMs = Math.abs(d2 - d1);
      const diffDays = Math.round(diffMs / 86400000) + 1;
      summary.textContent = `${diffDays} дн. (с ${formatISODate(state.modalRangeStart)} по ${formatISODate(state.modalRangeEnd)})`;
    } else {
      summary.textContent = "выберите начало и конец диапазона";
    }
  }
}

function onRangeInputChange() {
  const sVal = document.getElementById("rangeStartInput").value;
  const eVal = document.getElementById("rangeEndInput").value;
  if (sVal) state.modalRangeStart = sVal;
  if (eVal) state.modalRangeEnd = eVal;

  if (state.modalRangeStart && state.modalRangeEnd && state.modalRangeStart > state.modalRangeEnd) {
    const tmp = state.modalRangeStart;
    state.modalRangeStart = state.modalRangeEnd;
    state.modalRangeEnd = tmp;
    document.getElementById("rangeStartInput").value = state.modalRangeStart;
    document.getElementById("rangeEndInput").value = state.modalRangeEnd;
  }

  updateRangeInputWeekdays();
  renderDualCalendars();
}

function shiftDualCalendars(delta) {
  state.dualCalMonth += delta;
  if (state.dualCalMonth > 11) {
    state.dualCalMonth = 0;
    state.dualCalYear++;
  } else if (state.dualCalMonth < 0) {
    state.dualCalMonth = 11;
    state.dualCalYear--;
  }
  renderDualCalendars();
}

function renderDualCalendars() {
  const monthNames = [
    "Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
    "Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь"
  ];

  // Left calendar
  const leftY = state.dualCalYear;
  const leftM = state.dualCalMonth;
  const titleLeft = document.getElementById("dualCalTitleLeft");
  if (titleLeft) titleLeft.textContent = `${monthNames[leftM]} ${leftY}`;
  renderDualCalGrid("dualGridLeft", leftY, leftM);

  // Right calendar (next month)
  let rightY = leftY;
  let rightM = leftM + 1;
  if (rightM > 11) {
    rightM = 0;
    rightY++;
  }
  const titleRight = document.getElementById("dualCalTitleRight");
  if (titleRight) titleRight.textContent = `${monthNames[rightM]} ${rightY}`;
  renderDualCalGrid("dualGridRight", rightY, rightM);
}

function renderDualCalGrid(elementId, year, month) {
  const grid = document.getElementById(elementId);
  if (!grid) return;

  const dayHeaders = ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"];
  let html = dayHeaders.map(d => `<span class="dual-day-header">${d}</span>`).join("");

  const firstDay = new Date(year, month, 1);
  let startDayOfWeek = (firstDay.getDay() + 6) % 7;
  const daysInMonth = new Date(year, month + 1, 0).getDate();
  const daysInPrevMonth = new Date(year, month, 0).getDate();

  const todayISO = toISODate(new Date());

  // Prev month padding
  for (let i = startDayOfWeek - 1; i >= 0; i--) {
    const d = daysInPrevMonth - i;
    html += `<span class="dual-day-cell other-month">${d}</span>`;
  }

  // Days in month
  for (let d = 1; d <= daysInMonth; d++) {
    const iso = `${year}-${String(month + 1).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
    const isToday = (iso === todayISO);

    let inRange = false;
    let isStart = false;
    let isEnd = false;

    if (state.modalRangeStart && state.modalRangeEnd) {
      if (iso >= state.modalRangeStart && iso <= state.modalRangeEnd) {
        inRange = true;
      }
      if (iso === state.modalRangeStart) isStart = true;
      if (iso === state.modalRangeEnd) isEnd = true;
    } else if (state.modalRangeStart) {
      if (iso === state.modalRangeStart) isStart = true;
    }

    const classes = ["dual-day-cell"];
    if (isToday) classes.push("today");
    if (inRange) classes.push("in-range");
    if (isStart) classes.push("range-start");
    if (isEnd) classes.push("range-end");

    html += `<span class="${classes.join(' ')}" onclick="onDualCalDayClick('${iso}')">${d}</span>`;
  }

  // Next month padding
  const totalCells = startDayOfWeek + daysInMonth;
  const remainder = (7 - (totalCells % 7)) % 7;
  for (let i = 1; i <= remainder; i++) {
    html += `<span class="dual-day-cell other-month">${i}</span>`;
  }

  grid.innerHTML = html;
}

function onDualCalDayClick(iso) {
  if (!state.modalRangeStart || (state.modalRangeStart && state.modalRangeEnd)) {
    state.modalRangeStart = iso;
    state.modalRangeEnd = null;
  } else {
    if (iso < state.modalRangeStart) {
      state.modalRangeEnd = state.modalRangeStart;
      state.modalRangeStart = iso;
    } else {
      state.modalRangeEnd = iso;
    }
  }

  document.getElementById("rangeStartInput").value = state.modalRangeStart || "";
  document.getElementById("rangeEndInput").value = state.modalRangeEnd || "";

  updateRangeInputWeekdays();
  renderDualCalendars();
}

function applyPresetRange(preset) {
  const now = new Date();
  let start = "";
  let end = "";

  switch (preset) {
    case "today":
      start = toISODate(now);
      end = toISODate(now);
      break;
    case "this_week": {
      const b = getWeekBounds(now);
      start = toISODate(b.monday);
      end = toISODate(b.sunday);
      break;
    }
    case "last_week": {
      const prevWeekDate = new Date(now);
      prevWeekDate.setDate(now.getDate() - 7);
      const b = getWeekBounds(prevWeekDate);
      start = toISODate(b.monday);
      end = toISODate(b.sunday);
      break;
    }
    case "this_month": {
      const b = getMonthBounds(now.getFullYear(), now.getMonth());
      start = toISODate(b.first);
      end = toISODate(b.last);
      break;
    }
    case "last_month": {
      let m = now.getMonth() - 1;
      let y = now.getFullYear();
      if (m < 0) { m = 11; y--; }
      const b = getMonthBounds(y, m);
      start = toISODate(b.first);
      end = toISODate(b.last);
      break;
    }
    case "last_7_days": {
      const past7 = new Date(now);
      past7.setDate(now.getDate() - 6);
      start = toISODate(past7);
      end = toISODate(now);
      break;
    }
    case "last_30_days": {
      const past30 = new Date(now);
      past30.setDate(now.getDate() - 29);
      start = toISODate(past30);
      end = toISODate(now);
      break;
    }
    case "this_year": {
      start = `${now.getFullYear()}-01-01`;
      end = `${now.getFullYear()}-12-31`;
      break;
    }
  }

  state.modalRangeStart = start;
  state.modalRangeEnd = end;

  if (start) {
    const sDate = new Date(start + "T00:00:00");
    state.dualCalYear = sDate.getFullYear();
    state.dualCalMonth = sDate.getMonth();
  }

  document.getElementById("rangeStartInput").value = start;
  document.getElementById("rangeEndInput").value = end;

  updateRangeInputWeekdays();
  renderDualCalendars();
}

function confirmCustomRange() {
  const s = document.getElementById("rangeStartInput").value;
  const e = document.getElementById("rangeEndInput").value;

  if (!s || !e) {
    alert("Пожалуйста, укажите обе даты: 'От' и 'До'");
    return;
  }

  let start = s;
  let end = e;
  if (start > end) {
    start = e;
    end = s;
  }

  closeModal("modalRangePicker");
  setPeriod("range", { start, end });
}

// ----------------- YEARLY CALENDAR MODAL -----------------

function openYearCalendar() {
  state.yearViewYear = state.calYear;
  openModal("modalYearCalendar");
  loadAndRenderYearCalendar();
}

function changeYearView(delta) {
  state.yearViewYear += delta;
  loadAndRenderYearCalendar();
}

function onYearInputNumberChange(val) {
  const y = parseInt(val, 10);
  if (!isNaN(y) && y >= 1970 && y <= 2100) {
    state.yearViewYear = y;
    loadAndRenderYearCalendar();
  }
}

function goCurrentYear() {
  state.yearViewYear = new Date().getFullYear();
  loadAndRenderYearCalendar();
}

async function loadAndRenderYearCalendar() {
  const grid = document.getElementById("yearMonthsGrid");
  const title = document.getElementById("modalYearTitle");
  if (title) title.textContent = `${state.yearViewYear} год`;
  const yrInput = document.getElementById("yearInputNumber");
  if (yrInput) yrInput.value = state.yearViewYear;

  if (grid) {
    grid.innerHTML = '<div style="grid-column: 1/-1; text-align: center; padding: 40px; color: var(--text-dim);">Загрузка записей за год...</div>';
  }

  try {
    const start = `${state.yearViewYear}-01-01`;
    const end = `${state.yearViewYear}-12-31`;
    const data = await apiFetch(`/api/sessions?start=${start}&end=${end}`);
    const sessions = data.sessions || [];
    state.yearViewSessions = sessions;

    const countBadge = document.getElementById("yearTotalSessionsBadge");
    const hoursBadge = document.getElementById("yearTotalHoursBadge");
    if (countBadge) countBadge.textContent = `${data.count} записей`;
    if (hoursBadge) hoursBadge.textContent = `${data.total_hours.toFixed(1)} ч`;

    const monthCounts = new Array(12).fill(0);
    const eventDatesSet = new Set();

    for (const s of sessions) {
      const d = sessionDate(s);
      if (d) {
        eventDatesSet.add(d);
        const m = parseInt(d.slice(5, 7), 10) - 1;
        if (m >= 0 && m < 12) {
          monthCounts[m]++;
        }
      }
    }

    const monthNames = [
      "Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
      "Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь"
    ];
    const dayHeaders = ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"];
    const todayISO = toISODate(new Date());

    let html = "";
    for (let m = 0; m < 12; m++) {
      const count = monthCounts[m];
      const countText = count > 0 ? `${count} зап.` : '0';

      html += `
        <div class="year-month-card">
          <div class="year-month-hdr" onclick="jumpToMonth(${state.yearViewYear}, ${m})" title="Перейти к ${monthNames[m]} ${state.yearViewYear}">
            <span class="year-month-name">${monthNames[m]}</span>
            <span class="year-month-badge">${countText}</span>
          </div>
          <div class="year-month-grid">
            ${dayHeaders.map(dh => `<span class="year-day-header">${dh}</span>`).join("")}
      `;

      const firstDay = new Date(state.yearViewYear, m, 1);
      const startDow = (firstDay.getDay() + 6) % 7;
      const daysInMonth = new Date(state.yearViewYear, m + 1, 0).getDate();
      const daysInPrevMonth = new Date(state.yearViewYear, m, 0).getDate();

      // Prev month padding
      for (let i = startDow - 1; i >= 0; i--) {
        const pd = daysInPrevMonth - i;
        html += `<span class="year-day-cell other-month">${pd}</span>`;
      }

      // Days of month
      for (let d = 1; d <= daysInMonth; d++) {
        const iso = `${state.yearViewYear}-${String(m + 1).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
        const isToday = (iso === todayISO);
        const hasEv = eventDatesSet.has(iso);

        const classes = ["year-day-cell"];
        if (isToday) classes.push("today");
        if (hasEv) classes.push("has-events");

        html += `<span class="${classes.join(' ')}" onclick="jumpToDay('${iso}')" title="${formatISODate(iso)}${hasEv ? ' (есть записи)' : ''}">${d}</span>`;
      }

      // Next month padding
      const totalCells = startDow + daysInMonth;
      const remainder = (7 - (totalCells % 7)) % 7;
      for (let i = 1; i <= remainder; i++) {
        html += `<span class="year-day-cell other-month">${i}</span>`;
      }

      html += `
          </div>
        </div>
      `;
    }

    if (grid) grid.innerHTML = html;
  } catch (err) {
    if (grid) {
      grid.innerHTML = `<div style="grid-column: 1/-1; text-align: center; padding: 30px; color: var(--accent-red);">Ошибка загрузки: ${err.message}</div>`;
    }
  }
}

function jumpToMonth(year, month) {
  state.calYear = year;
  state.calMonth = month;
  closeModal("modalYearCalendar");
  setPeriod("month");
}

function jumpToDay(iso) {
  const y = parseInt(iso.slice(0, 4), 10);
  const m = parseInt(iso.slice(5, 7), 10) - 1;
  state.calYear = y;
  state.calMonth = m;
  closeModal("modalYearCalendar");
  selectCalendarDate(iso);
}

function toggleTypeFilter(t) {
  if (state.selectedTypes.has(t)) {
    state.selectedTypes.delete(t);
  } else {
    state.selectedTypes.add(t);
  }
  renderTypesFilter();
  refreshData();
}

function toggleAllTypes(checkAll) {
  if (checkAll) {
    state.selectedTypes.clear();
    state.types.forEach(t => state.selectedTypes.add(t));
  } else {
    state.selectedTypes.clear();
  }
  renderTypesFilter();
  refreshData();
}

function onSearchInput(val) {
  state.searchQuery = val.trim();
  debounce(refreshData, 250)();
}

// ======================== BATCH SELECTION & OPERATIONS ========================

function onToggleRowSelect(e, id) {
  if (e.target.checked) {
    state.selectedBatchIds.add(id);
  } else {
    state.selectedBatchIds.delete(id);
  }
  const row = document.querySelector(`.session-row[data-id="${id}"]`);
  if (row) {
    row.classList.toggle("batch-selected", e.target.checked);
  }
  updateBatchUI();
}

function toggleSelectAllFiltered() {
  const allFilteredIds = state.sessions.map(s => s.id);
  if (allFilteredIds.length === 0) return;

  const allSelected = allFilteredIds.every(id => state.selectedBatchIds.has(id));
  if (allSelected) {
    state.selectedBatchIds.clear();
  } else {
    allFilteredIds.forEach(id => state.selectedBatchIds.add(id));
  }
  renderFeed();
}

function clearSelection() {
  state.selectedBatchIds.clear();
  renderFeed();
}

function updateBatchUI() {
  const bar = document.getElementById("batchActionBar");
  const countEl = document.getElementById("batchCountBadge");
  const btnSelectAllIcon = document.getElementById("btnSelectAllIcon");
  const count = state.selectedBatchIds.size;

  if (bar) {
    bar.style.display = count > 0 ? "flex" : "none";
  }
  if (countEl) {
    countEl.textContent = `Выбрано: ${count}`;
  }
  if (btnSelectAllIcon) {
    const allFiltered = state.sessions.length > 0 && state.sessions.every(s => state.selectedBatchIds.has(s.id));
    btnSelectAllIcon.textContent = allFiltered ? "☑️" : "☐";
  }
}

function openBatchStatusPicker(e) {
  e.preventDefault();
  e.stopPropagation();
  if (state.selectedBatchIds.size === 0) return;

  state.isBatchStatusPicker = true;
  state.statusPopupTargetId = null;

  const btn = e.currentTarget;
  const rect = btn.getBoundingClientRect();
  const popup = document.getElementById("statusPickerPopup");

  renderStatusPickerPopup();

  popup.style.display = "block";
  popup.style.left = `${Math.min(rect.left + window.scrollX, window.innerWidth - 220)}px`;
  popup.style.top = `${rect.bottom + window.scrollY + 6}px`;

  popup.setAttribute("tabindex", "-1");
  popup.focus();
}

function openBatchEditFieldModal() {
  if (state.selectedBatchIds.size === 0) return;
  const count = state.selectedBatchIds.size;
  const sub = document.getElementById("batchEditSub");
  if (sub) sub.textContent = `Будет изменено записей: ${count}`;

  const select = document.getElementById("batchFieldSelect");
  if (select) {
    select.value = "status";
  }
  onBatchFieldSelectChange("status");
  openModal("modalBatchEditField");
}

function onBatchFieldSelectChange(field) {
  const container = document.getElementById("batchValueContainer");
  if (!container) return;

  switch (field) {
    case "status":
      container.innerHTML = `
        <label for="batchFieldValue" class="form-label">Новый статус:</label>
        <select id="batchFieldValue" class="form-select">
          ${state.statuses.map(st => `<option value="${escapeHtml(st)}">${escapeHtml(st)}</option>`).join("")}
        </select>
      `;
      break;
    case "cash":
      container.innerHTML = `
        <label for="batchFieldValue" class="form-label">Новый кэш:</label>
        <input type="text" id="batchFieldValue" class="form-input" value="0.0" placeholder="0.0">
      `;
      break;
    case "name":
      container.innerHTML = `
        <label for="batchFieldValue" class="form-label">Новое наименование:</label>
        <input type="text" id="batchFieldValue" class="form-input" required placeholder="Название события">
      `;
      break;
    case "type":
      container.innerHTML = `
        <label for="batchFieldValue" class="form-label">Новый тип записи:</label>
        <select id="batchFieldValue" class="form-select">
          ${state.types.map(t => `<option value="${escapeHtml(t)}">${escapeHtml(t)}</option>`).join("")}
        </select>
      `;
      break;
    case "time":
      container.innerHTML = `
        <label for="batchFieldValue" class="form-label">Новое время начала (ЧЧ:ММ):</label>
        <input type="time" id="batchFieldValue" class="form-input" required value="10:00">
      `;
      break;
    case "duration":
      container.innerHTML = `
        <label for="batchFieldValue" class="form-label">Новая длительность (в минутах):</label>
        <input type="number" id="batchFieldValue" class="form-input" min="1" max="1440" required value="60">
      `;
      break;
    case "date":
      const today = new Date().toISOString().slice(0, 10);
      container.innerHTML = `
        <label for="batchFieldValue" class="form-label">Новая дата (перенос записей):</label>
        <input type="date" id="batchFieldValue" class="form-input" required value="${today}">
      `;
      break;
    case "notes":
      container.innerHTML = `
        <label for="batchFieldValue" class="form-label">Новый комментарий / заметка:</label>
        <textarea id="batchFieldValue" class="form-textarea" rows="3" placeholder="Текст комментария"></textarea>
      `;
      break;
  }
  const input = document.getElementById("batchFieldValue");
  if (input) input.focus();
}

async function handleBatchEditSubmit(e) {
  e.preventDefault();
  const ids = Array.from(state.selectedBatchIds);
  if (ids.length === 0) return;

  const field = document.getElementById("batchFieldSelect").value;
  const valEl = document.getElementById("batchFieldValue");
  let value = valEl ? valEl.value : "";

  if (field === "cash") {
    value = parseCash(value);
  } else if (field === "duration") {
    value = parseInt(value, 10) || 0;
  }

  try {
    const res = await apiFetch("/api/sessions/batch", {
      method: "PATCH",
      body: JSON.stringify({ ids, field, value }),
    });
    closeModal("modalBatchEditField");
    state.selectedBatchIds.clear();
    await fetchTypes();
    await refreshData();
  } catch (err) {
    alert("Ошибка группового изменения: " + err.message);
  }
}

function openBatchDeleteModal() {
  if (state.selectedBatchIds.size === 0) return;
  const count = state.selectedBatchIds.size;
  const countEl = document.getElementById("batchDelCount");
  if (countEl) countEl.textContent = count;

  const listEl = document.getElementById("batchDelList");
  const selectedSessions = state.sessions.filter(s => state.selectedBatchIds.has(s.id));

  let html = "";
  for (const s of selectedSessions) {
    html += `
      <div class="batch-del-item">
        <span class="batch-del-item-name">${escapeHtml(s.name)}</span>
        <span class="batch-del-item-meta">${formatISODate(sessionDate(s))} ${s.time} • ${escapeHtml(s.type || '(без типа)')}</span>
      </div>
    `;
  }
  if (listEl) listEl.innerHTML = html;
  openModal("modalBatchDelete");
}

async function confirmBatchDelete() {
  const ids = Array.from(state.selectedBatchIds);
  if (ids.length === 0) return;

  try {
    await apiFetch("/api/sessions/batch", {
      method: "DELETE",
      body: JSON.stringify({ ids }),
    });
    closeModal("modalBatchDelete");
    state.selectedBatchIds.clear();
    if (state.focusedId && ids.includes(state.focusedId)) {
      state.focusedId = null;
    }
    await refreshData();
  } catch (err) {
    alert("Ошибка группового удаления: " + err.message);
  }
}

// ======================== MODAL DIALOGS ========================

function openModal(id) {
  const d = document.getElementById(id);
  if (d && typeof d.showModal === "function") {
    d.showModal();
  }
}

function closeModal(id) {
  const d = document.getElementById(id);
  if (d && typeof d.close === "function") {
    d.close();
  }
}

// ----------------- ADD / EDIT SESSION -----------------

function openAddModal() {
  document.getElementById("modalSessionTitle").textContent = "Новая запись";
  document.getElementById("formSessionId").value = "";

  const todayIso = state.selectedDate || new Date().toISOString().slice(0, 10);
  document.getElementById("formDate").value = todayIso;
  document.getElementById("formTime").value = "10:00";
  document.getElementById("formName").value = "";
  updateTypeSelect(state.types[0] || "Стрельба из лука");
  document.getElementById("formDuration").value = state.config.default_duration || 60;
  document.getElementById("formCash").value = "0.0";
  updateStatusSelect("Активно");
  document.getElementById("formNotes").value = "";

  document.getElementById("repeatSection").style.display = "block";
  document.getElementById("formRepeat").value = "none";
  document.getElementById("repeatUntilGroup").style.display = "none";
  document.getElementById("seriesEditSection").style.display = "none";

  openModal("modalSession");
}

function editFocusedSession() {
  if (!state.focusedId) return;
  const s = state.sessions.find(x => x.id === state.focusedId);
  if (!s) return;

  document.getElementById("modalSessionTitle").textContent = `Редактирование (${s.id})`;
  document.getElementById("formSessionId").value = s.id;
  document.getElementById("formDate").value = sessionDate(s);
  document.getElementById("formTime").value = s.time;
  document.getElementById("formName").value = s.name;
  updateTypeSelect(s.type);
  document.getElementById("formDuration").value = s.duration;
  document.getElementById("formCash").value = s.cash !== undefined ? s.cash : 0.0;
  updateStatusSelect(s.status || "Активно");
  document.getElementById("formNotes").value = s.notes || "";

  document.getElementById("repeatSection").style.display = "none"; // cannot change repeat rule in edit
  document.getElementById("seriesEditSection").style.display = s.series_id ? "block" : "none";

  openModal("modalSession");
}

async function editFocusedField(field) {
  if (!state.focusedId) return;
  const s = state.sessions.find(x => x.id === state.focusedId);
  if (!s) return;

  let newVal = null;
  switch (field) {
    case "name":
      newVal = prompt("Введите новое название записи:", s.name);
      break;
    case "time":
      newVal = prompt("Введите новое время записи (ЧЧ:ММ):", s.time);
      break;
    case "duration":
      newVal = prompt("Введите продолжительность в минутах:", s.duration);
      break;
    case "cash":
      newVal = prompt("Введите значение кэша:", s.cash !== undefined ? s.cash : 0.0);
      break;
    case "type":
      newVal = prompt("Введите тип записи:", s.type);
      break;
    case "notes":
      newVal = prompt("Введите комментарий / заметку:", s.notes || "");
      break;
    case "date":
      editFocusedSession();
      return;
  }

  if (newVal !== null) {
    try {
      if (field === "cash") {
        newVal = parseCash(newVal);
      }
      await apiFetch(`/api/sessions/${s.id}/field`, {
        method: "PATCH",
        body: JSON.stringify({ field, value: newVal }),
      });
      await refreshData();
    } catch (err) {
      alert("Ошибка обновления поля: " + err.message);
    }
  }
}

function onRepeatSelectChange(mode) {
  const untilGroup = document.getElementById("repeatUntilGroup");
  const untilInput = document.getElementById("formRepeatUntil");
  if (mode === "none") {
    untilGroup.style.display = "none";
  } else {
    untilGroup.style.display = "block";
    if (!untilInput.value) {
      const d = new Date();
      d.setMonth(d.getMonth() + 1);
      untilInput.value = d.toISOString().slice(0, 10);
    }
  }
}

async function handleSessionFormSubmit(e) {
  e.preventDefault();
  const id = document.getElementById("formSessionId").value;
  const isEdit = !!id;

  const date = document.getElementById("formDate").value;
  const time = document.getElementById("formTime").value;
  const name = document.getElementById("formName").value;
  const type = document.getElementById("formType").value;
  const duration = parseInt(document.getElementById("formDuration").value, 10);
  const cash = parseCash(document.getElementById("formCash").value);
  const status = document.getElementById("formStatus").value;
  const notes = document.getElementById("formNotes").value;

  try {
    if (!isEdit) {
      // Create new session
      const repeat = document.getElementById("formRepeat").value;
      const repeat_until = document.getElementById("formRepeatUntil").value;

      const res = await apiFetch("/api/sessions", {
        method: "POST",
        body: JSON.stringify({ date, time, name, type, duration, cash, status, notes, repeat, repeat_until }),
      });
      closeModal("modalSession");
      await fetchTypes(); // refresh types in case a new one was added
      await refreshData();
      if (res.first_id) setFocusedSession(res.first_id);
    } else {
      // Update existing session
      const updateSeries = document.querySelector('input[name="editSeriesMode"]:checked')?.value === "series";

      await apiFetch(`/api/sessions/${id}`, {
        method: "PUT",
        body: JSON.stringify({ date, time, name, type, duration, cash, status, notes, update_series: updateSeries }),
      });
      closeModal("modalSession");
      await fetchTypes();
      await refreshData();
      setFocusedSession(id);
    }
  } catch (err) {
    alert("Ошибка сохранения: " + err.message);
  }
}

// ----------------- DELETE SESSION -----------------

function deleteFocusedSession() {
  if (!state.focusedId) return;
  const s = state.sessions.find(x => x.id === state.focusedId);
  if (!s) return;

  document.getElementById("delItemName").textContent = s.name;
  document.getElementById("delItemMeta").textContent = `${formatISODate(sessionDate(s))} ${s.time} • ${s.duration} мин • ${s.type || '(без типа)'}`;
  document.getElementById("delSeriesOptions").style.display = s.series_id ? "block" : "none";

  openModal("modalDelete");
}

async function confirmDeleteSession() {
  const id = state.focusedId;
  if (!id) return;

  const s = state.sessions.find(x => x.id === id);
  const isSeriesChoice = s && s.series_id && document.querySelector('input[name="delSeriesChoice"]:checked')?.value === "series";

  try {
    await apiFetch(`/api/sessions/${id}?series=${isSeriesChoice ? 'true' : 'false'}`, {
      method: "DELETE",
    });
    closeModal("modalDelete");
    state.focusedId = null;
    await refreshData();
  } catch (err) {
    alert("Не удалось удалить запись: " + err.message);
  }
}

// ----------------- DUPLICATE SESSION -----------------

async function duplicateFocusedSession() {
  if (!state.focusedId) return;
  try {
    const res = await apiFetch(`/api/sessions/${state.focusedId}/duplicate`, {
      method: "POST",
    });
    await refreshData();
    if (res.session && res.session.id) {
      setFocusedSession(res.session.id);
    }
  } catch (err) {
    alert("Не удалось дублировать запись: " + err.message);
  }
}

// ----------------- TYPE MODAL -----------------

function openTypeModal() {
  const list = document.getElementById("modalTypeList");
  let html = "";
  for (const t of state.types) {
    const isChecked = state.selectedTypes.size === 0 || state.selectedTypes.has(t);
    const count = state.sessions.filter(s => s.type === t).length;
    html += `
      <div class="type-filter-row ${isChecked ? 'active' : ''}" onclick="this.classList.toggle('active')">
        <div class="type-label-left">
          <span class="cb-icon">[✓]</span>
          <span>${escapeHtml(t)}</span>
        </div>
        <span class="type-count-badge">${count} зап.</span>
      </div>
    `;
  }
  list.innerHTML = html;
  openModal("modalTypes");
}

function filterModalTypeList(query) {
  query = query.toLowerCase().trim();
  document.querySelectorAll("#modalTypeList .type-filter-row").forEach(row => {
    const txt = row.textContent.toLowerCase();
    row.style.display = txt.includes(query) ? "flex" : "none";
  });
}

function checkAllModalTypes(checkAll) {
  document.querySelectorAll("#modalTypeList .type-filter-row").forEach(row => {
    row.classList.toggle("active", checkAll);
  });
}

function applyModalTypes() {
  state.selectedTypes.clear();
  document.querySelectorAll("#modalTypeList .type-filter-row.active").forEach(row => {
    const name = row.querySelector(".type-label-left span:last-child")?.textContent?.trim();
    if (name) state.selectedTypes.add(name);
  });
  closeModal("modalTypes");
  renderTypesFilter();
  refreshData();
}

// ----------------- SETTINGS MODAL & SERVICE -----------------

function openSettingsModal() {
  fetchServiceStatus();
  openModal("modalSettings");
}

async function saveSettings() {
  const default_mode = document.getElementById("cfgDefaultMode") ? document.getElementById("cfgDefaultMode").value : "cli";
  const web_port = (document.getElementById("cfgWebPort") ? parseInt(document.getElementById("cfgWebPort").value, 10) : 8080) || 8080;
  const default_duration = parseInt(document.getElementById("cfgDefaultDuration").value, 10) || 60;
  const date_check_mode = document.getElementById("cfgDateCheckMode").value;
  const ask_on_conflict = document.getElementById("cfgAskConflict").value === "true";

  try {
    const updated = {
      ...state.config,
      default_mode,
      web_port,
      default_duration,
      date_check_mode,
      ask_on_conflict,
    };
    await apiFetch("/api/config", {
      method: "POST",
      body: JSON.stringify(updated),
    });
    state.config = updated;
    closeModal("modalSettings");
    alert("Настройки успешно сохранены!");
  } catch (err) {
    alert("Ошибка сохранения настроек: " + err.message);
  }
}

async function fetchServiceStatus() {
  const badge = document.getElementById("serviceStatusBadge");
  const btnInstall = document.getElementById("btnServiceInstall");
  const btnRestart = document.getElementById("btnServiceRestart");
  const btnStop = document.getElementById("btnServiceStop");
  const btnUninstall = document.getElementById("btnServiceUninstall");
  const detailsBox = document.getElementById("serviceDetailsBox");

  if (!badge) return;
  badge.textContent = "Проверка...";
  badge.className = "badge-pill stat-blue";

  try {
    const info = await apiFetch("/api/service/status");
    if (info.running) {
      badge.textContent = "● Служба активна (работает)";
      badge.className = "badge-pill stat-green";
      btnInstall.style.display = "none";
      btnRestart.style.display = "inline-block";
      btnStop.style.display = "inline-block";
      btnUninstall.style.display = "inline-block";
    } else if (info.installed) {
      badge.textContent = "○ Служба остановлена";
      badge.className = "badge-pill stat-yellow";
      btnInstall.textContent = "Запустить службу";
      btnInstall.style.display = "inline-block";
      btnRestart.style.display = "none";
      btnStop.style.display = "none";
      btnUninstall.style.display = "inline-block";
    } else {
      badge.textContent = "✕ Служба не установлена";
      badge.className = "badge-pill";
      btnInstall.textContent = "Установить и запустить службу";
      btnInstall.style.display = "inline-block";
      btnRestart.style.display = "none";
      btnStop.style.display = "none";
      btnUninstall.style.display = "none";
    }

    if (info.details) {
      detailsBox.style.display = "block";
      detailsBox.textContent = info.platform + (info.unit_path ? "\nЮнит: " + info.unit_path : "") + "\n\n" + info.details;
    } else {
      detailsBox.style.display = "none";
    }
  } catch (err) {
    badge.textContent = "Ошибка получения статуса";
    badge.className = "badge-pill stat-red";
  }
}

async function serviceAction(action) {
  const port = parseInt(document.getElementById("cfgWebPort").value, 10) || 8080;
  try {
    const res = await apiFetch("/api/service/action", {
      method: "POST",
      body: JSON.stringify({ action, port }),
    });
    if (res.ok) {
      alert("Команда службы выполнена успешно!");
      await fetchServiceStatus();
    } else {
      alert("Ошибка: " + (res.error || "Неизвестная ошибка"));
    }
  } catch (err) {
    alert("Ошибка службы: " + err.message);
  }
}

// ----------------- THEME & RESPONSIVE TABS -----------------

function changeTheme(theme) {
  if (theme === "tokyo") {
    document.documentElement.removeAttribute("data-theme");
  } else {
    document.documentElement.setAttribute("data-theme", theme);
  }
}

function switchMobileTab(tab) {
  state.mobileTab = tab;
  document.getElementById("mainWorkspace").setAttribute("data-mobile-tab", tab);
  document.querySelectorAll(".mobile-nav-btn").forEach(btn => {
    btn.classList.toggle("active", btn.dataset.tab === tab);
  });
}

// ======================== KEYBOARD NAVIGATION ========================

function initGlobalKeyboard() {
  window.addEventListener("keydown", (e) => {
    // If inside a text input / textarea, only allow Escape
    if (["INPUT", "TEXTAREA", "SELECT"].includes(document.activeElement.tagName)) {
      if (e.key === "Escape") {
        document.activeElement.blur();
        hideStatusPopup();
        hideContextMenu();
      }
      return;
    }

    // Modal open -> let Esc close it
    if (document.querySelector("dialog[open]")) {
      return;
    }

    switch (e.key) {
      case "ArrowDown":
      case "j":
        e.preventDefault();
        moveFocusedSession(1);
        break;
      case "ArrowUp":
      case "k":
        e.preventDefault();
        moveFocusedSession(-1);
        break;
      case "a":
      case "а":
        e.preventDefault();
        openAddModal();
        break;
      case "e":
      case "е":
      case "\\":
        e.preventDefault();
        editFocusedSession();
        break;
      case "d":
      case "в":
        e.preventDefault();
        deleteFocusedSession();
        break;
      case "t":
      case "т":
        e.preventDefault();
        openTypeModal();
        break;
      case "/":
        e.preventDefault();
        const inp = document.getElementById("topSearchInput");
        inp.focus();
        inp.select();
        break;
      case "s":
      case "ы":
        e.preventDefault();
        openSettingsModal();
        break;
    }
  });
}

function moveFocusedSession(dir) {
  if (state.sessions.length === 0) return;
  const curIdx = state.sessions.findIndex(s => s.id === state.focusedId);
  let nextIdx = curIdx + dir;
  if (nextIdx < 0) nextIdx = 0;
  if (nextIdx >= state.sessions.length) nextIdx = state.sessions.length - 1;

  const nextId = state.sessions[nextIdx].id;
  setFocusedSession(nextId);

  const row = document.querySelector(`.session-row[data-id="${nextId}"]`);
  if (row) row.scrollIntoView({ block: "nearest" });
}

// ======================== UTILITIES ========================

function toISODate(d) {
  if (!d) return "";
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

function getWeekBounds(refDate = new Date()) {
  const d = new Date(refDate);
  const day = (d.getDay() + 6) % 7; // 0 = Mon, 6 = Sun
  const monday = new Date(d);
  monday.setDate(d.getDate() - day);
  monday.setHours(0, 0, 0, 0);
  const sunday = new Date(monday);
  sunday.setDate(monday.getDate() + 6);
  sunday.setHours(23, 59, 59, 999);
  return { monday, sunday };
}

function getMonthBounds(year, month) {
  const first = new Date(year, month, 1);
  const last = new Date(year, month + 1, 0);
  return { first, last };
}

function getISOWeekNumber(d) {
  const date = new Date(Date.UTC(d.getFullYear(), d.getMonth(), d.getDate()));
  const dayNum = date.getUTCDay() || 7;
  date.setUTCDate(date.getUTCDate() + 4 - dayNum);
  const yearStart = new Date(Date.UTC(date.getUTCFullYear(), 0, 1));
  return Math.ceil((((date - yearStart) / 86400000) + 1) / 7);
}

function formatShortDate(iso) {
  if (!iso || iso.length < 10) return iso;
  const parts = iso.split("-");
  return `${parts[2]}.${parts[1]}`;
}

function getWeekdayShort(iso) {
  if (!iso) return "";
  const d = new Date(iso + "T00:00:00");
  const days = ["Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"];
  return days[d.getDay()];
}

function sessionDate(s) {
  if (s.id && s.id.length >= 8) {
    return `${s.id.slice(0,4)}-${s.id.slice(4,6)}-${s.id.slice(6,8)}`;
  }
  return new Date().toISOString().slice(0, 10);
}

function formatISODate(iso) {
  if (!iso || iso.length < 10) return iso;
  const parts = iso.split("-");
  return `${parts[2]}.${parts[1]}.${parts[0]}`;
}

function formatDateHeader(iso) {
  if (!iso || iso.length < 10) return iso;
  const d = new Date(iso + "T00:00:00");
  const weekdays = ["Воскресенье", "Понедельник", "Вторник", "Среда", "Четверг", "Пятница", "Суббота"];
  const dayName = weekdays[d.getDay()];

  const today = new Date().toISOString().slice(0, 10);
  const isToday = iso === today;

  return `${formatISODate(iso)}, ${dayName} ${isToday ? '(Сегодня)' : ''}`;
}

function getStatusClass(status) {
  if (!status) return "status-empty";
  const s = status.toLowerCase();
  if (s.includes("актив") || s.includes("выполн")) return "status-active";
  if (s.includes("пропущ")) return "status-skip";
  if (s.includes("отмен")) return "status-cancel";
  return "status-active";
}

function escapeHtml(str) {
  if (!str) return "";
  return String(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}

function debounce(fn, wait) {
  let timer;
  return function (...args) {
    clearTimeout(timer);
    timer = setTimeout(() => fn.apply(this, args), wait);
  };
}

function parseCash(val) {
  if (val === undefined || val === null || val === "") return 0.0;
  if (typeof val === "number") return isNaN(val) ? 0.0 : val;
  const s = String(val).replace(",", ".").trim();
  const num = parseFloat(s);
  return isNaN(num) ? 0.0 : Math.round(num * 100) / 100;
}

function formatCash(val) {
  const n = parseCash(val);
  return n.toLocaleString("ru-RU", { minimumFractionDigits: 1, maximumFractionDigits: 2 });
}
