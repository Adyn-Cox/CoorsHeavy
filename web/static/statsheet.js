// Stat sheet: transcribe a paper scorebook one game at a time and save it.
//
// The grid opens on whatever the server already has for a game, so editing a
// night that has been typed is the same act as typing it the first time. Typing
// only touches localStorage; nothing reaches the database until Save. That
// keeps a half-typed game across a refresh, and means the sheet can tell you
// when what you are looking at is not what is stored.
(function () {
  var root = document.getElementById('statsheet-root');
  var dataEl = document.getElementById('statsheet-data');
  if (!root || !dataEl) return;

  var data = JSON.parse(dataEl.textContent);
  var COLS = data.columns;
  var LABELS = { ab: 'AB', r: 'R', h: 'H', '2b': '2B', '3b': '3B', hr: 'HR',
                 rbi: 'RBI', bb: 'BB', so: 'SO', sf: 'SF', e: 'E' };
  var STORE_KEY = 'coorsheavy:statsheet:v1:' + data.season.id;

  // The roster has had duplicate first names (two Patty, two Parker). Two
  // identical rows in a transcription grid is how a line ends up on the wrong
  // player, so show the slug alongside any name that isn't unique.
  var nameCount = {};
  data.roster.forEach(function (p) { nameCount[p.name] = (nameCount[p.name] || 0) + 1; });
  function displayName(p) { return nameCount[p.name] > 1 ? p.name + ' (' + p.slug + ')' : p.name; }

  // --- persistence ---------------------------------------------------------

  function loadAll() {
    try { return JSON.parse(localStorage.getItem(STORE_KEY)) || {}; }
    catch (e) { return {}; }  // private window, cleared storage, quota errors
  }

  function saveAll(all) {
    try { localStorage.setItem(STORE_KEY, JSON.stringify(all)); }
    catch (e) { flash('Could not keep a local draft — save before closing this tab.', true); }
  }

  // sheets holds the local draft for every game, seeded from what the server
  // stored. A draft left over from a previous visit wins — it is the newer of
  // the two, and it is the one thing here that isn't saved anywhere else.
  var sheets = loadAll();
  function gameKey(g) { return g.date + '|' + g.opponent; }
  data.games.forEach(function (g) {
    var k = gameKey(g);
    if (!sheets[k]) sheets[k] = normalise(g.lines);
  });

  function sheetFor(g) { return sheets[gameKey(g)] || (sheets[gameKey(g)] = {}); }
  function lineFor(g, slug) {
    var s = sheetFor(g);
    return s[slug] || (s[slug] = {});
  }

  // normalise drops zero-only lines and blank columns so a draft and the
  // server's copy of the same numbers compare equal.
  function normalise(lines) {
    var out = {};
    Object.keys(lines || {}).forEach(function (slug) {
      var line = lines[slug];
      if (isEmpty(line) && n(line, 'rbi') === 0) return;
      var clean = {};
      COLS.forEach(function (c) { if (n(line, c) !== 0) clean[c] = n(line, c); });
      out[slug] = clean;
    });
    return out;
  }

  // dirty reports whether the grid shows something the database doesn't.
  function dirty(g) {
    return JSON.stringify(normalise(sheetFor(g))) !== JSON.stringify(normalise(g.lines));
  }

  // --- stat maths (mirrors store.Batting) ----------------------------------

  function n(line, col) { return parseInt(line[col], 10) || 0; }

  // Mirrors store.Batting exactly, including the league key's two departures
  // from standard baseball: PA is at-bats plus walks (no sac-fly bucket), and
  // OB is hits plus walks. NaN means undefined and renders as a dash.
  function derived(line) {
    var ab = n(line, 'ab'), h = n(line, 'h'), bb = n(line, 'bb');
    var b2 = n(line, '2b'), b3 = n(line, '3b'), hr = n(line, 'hr');
    var singles = h - b2 - b3 - hr;
    var tb = singles + 2 * b2 + 3 * b3 + 4 * hr;
    var pa = ab + bb;
    var obp = ratio(h + bb, pa);
    var slg = ratio(tb, ab);
    return {
      avg: ratio(h, ab),
      obp: obp,
      slg: slg,
      // A player who never makes an out never yields the 21st out, so the
      // runs-per-7 estimate diverges rather than being zero.
      rp7: (isNaN(obp) || obp >= 1) ? NaN : ((21 / (1 - obp)) * slg) / 3
    };
  }

  function ratio(num, den) { return den > 0 ? num / den : NaN; }

  function fmtRate(f) {
    if (isNaN(f) || !isFinite(f)) return '—';
    var s = f.toFixed(3);
    return s.charAt(0) === '0' ? s.slice(1) : s;
  }

  function fmtCount(f) {
    if (isNaN(f) || !isFinite(f)) return '—';
    return f.toFixed(1);
  }

  // Same checks as store.Batting.Validate, so nothing that passes here is
  // rejected at import time.
  function problems(line) {
    var ab = n(line, 'ab'), h = n(line, 'h'), so = n(line, 'so');
    var xbh = n(line, '2b') + n(line, '3b') + n(line, 'hr');
    var bad = {};
    if (h > ab) { bad.h = 'H cannot exceed AB'; bad.ab = 'H cannot exceed AB'; }
    if (xbh > h) { bad['2b'] = bad['3b'] = bad.hr = '2B+3B+HR cannot exceed H'; }
    if (so > ab) { bad.so = 'SO cannot exceed AB'; }
    COLS.forEach(function (c) { if (n(line, c) < 0) bad[c] = 'cannot be negative'; });
    return bad;
  }

  function isEmpty(line) {
    return COLS.every(function (c) { return n(line, c) === 0; });
  }

  // --- rendering -----------------------------------------------------------

  var current = data.games.filter(function (g) { return g.id === data.selected; })[0]
             || data.games[0];

  function gamesWithData() {
    return data.games.filter(function (g) {
      var s = sheets[gameKey(g)];
      return s && Object.keys(s).some(function (slug) { return !isEmpty(s[slug]); });
    });
  }

  function render() {
    root.innerHTML = '';
    root.appendChild(gamePicker());
    if (!current) {
      root.appendChild(el('p', 'text-gray-400', 'No games on the schedule for this season yet.'));
      return;
    }
    root.appendChild(grid());
    root.appendChild(actions());
    // Only now is the actions row in the document, so the state indicator can
    // be found and filled in.
    updateState();
  }

  function gamePicker() {
    var wrap = el('div', 'flex flex-wrap items-center gap-2');
    wrap.appendChild(el('span', 'text-xs font-semibold uppercase tracking-widest text-gray-500', 'Game'));

    var sel = document.createElement('select');
    sel.className = 'border border-white bg-black px-2 py-1 text-sm text-white';
    data.games.forEach(function (g) {
      var o = document.createElement('option');
      o.value = gameKey(g);
      var s = sheets[gameKey(g)];
      var filled = s && Object.keys(s).some(function (k) { return !isEmpty(s[k]); });
      o.textContent = g.label + '  ' + (g.home ? 'vs ' : '@ ') + g.opponent + (filled ? '  ✓' : '');
      if (current && gameKey(g) === gameKey(current)) o.selected = true;
      sel.appendChild(o);
    });
    sel.addEventListener('change', function () {
      current = data.games.filter(function (g) { return gameKey(g) === sel.value; })[0];
      render();
    });
    wrap.appendChild(sel);

    if (current && current.played) {
      wrap.appendChild(el('span', 'text-xs text-gray-500',
        'Final: ' + current.us + '-' + current.them));
    }
    return wrap;
  }

  function grid() {
    var box = el('div', 'overflow-x-auto');
    var table = document.createElement('table');
    table.className = 'w-full border-collapse border border-white text-right text-sm';

    var thead = document.createElement('thead');
    var hr = document.createElement('tr');
    hr.className = 'bg-white text-black';
    hr.appendChild(th('Player', 'text-left'));
    COLS.forEach(function (c) { hr.appendChild(th(LABELS[c])); });
    ['Avg', 'OB%', 'Slug', 'RP7'].forEach(function (c) { hr.appendChild(th(c)); });
    thead.appendChild(hr);
    table.appendChild(thead);

    var tbody = document.createElement('tbody');
    data.roster.forEach(function (p, rowIdx) {
      tbody.appendChild(playerRow(p, rowIdx));
    });
    table.appendChild(tbody);
    table.appendChild(totalsFoot());
    box.appendChild(table);
    return box;
  }

  function playerRow(p, rowIdx) {
    var line = lineFor(current, p.slug);
    var tr = document.createElement('tr');
    tr.className = 'border-b border-gray-700';
    tr.dataset.slug = p.slug;

    var nameCell = document.createElement('td');
    nameCell.className = 'px-2 py-1 text-left whitespace-nowrap';
    nameCell.textContent = displayName(p);
    tr.appendChild(nameCell);

    COLS.forEach(function (c, colIdx) {
      var td = document.createElement('td');
      td.className = 'p-0';
      var input = document.createElement('input');
      input.type = 'text';
      input.inputMode = 'numeric';
      input.className = 'w-12 border-0 bg-black px-1 py-1 text-right text-white focus:bg-gray-900 focus:outline focus:outline-1 focus:outline-white';
      input.value = line[c] === undefined || line[c] === 0 ? '' : line[c];
      input.dataset.slug = p.slug;
      input.dataset.col = c;
      input.dataset.row = rowIdx;
      input.dataset.colIdx = colIdx;
      input.addEventListener('input', onInput);
      input.addEventListener('keydown', onKeyDown);
      input.addEventListener('focus', function () { input.select(); });
      td.appendChild(input);
      tr.appendChild(td);
    });

    ['avg', 'obp', 'slg', 'rp7'].forEach(function (k) {
      var td = document.createElement('td');
      td.className = 'px-2 py-1 text-gray-400';
      td.dataset.rate = k;
      tr.appendChild(td);
    });

    updateRow(tr);
    return tr;
  }

  function totalsFoot() {
    var tfoot = document.createElement('tfoot');
    var tr = document.createElement('tr');
    tr.id = 'sheet-totals';
    tr.className = 'border-t-2 border-white font-bold';
    tr.appendChild(td('Team', 'px-2 py-1 text-left uppercase tracking-wide'));
    COLS.forEach(function (c) {
      var cell = td('0', 'px-2 py-1');
      cell.dataset.total = c;
      tr.appendChild(cell);
    });
    for (var i = 0; i < 4; i++) tr.appendChild(td('', 'px-2 py-1'));
    tfoot.appendChild(tr);

    var warn = document.createElement('tr');
    warn.id = 'sheet-warning';
    var wtd = document.createElement('td');
    wtd.colSpan = COLS.length + 5;
    wtd.className = 'px-2 py-2 text-left text-sm';
    warn.appendChild(wtd);
    tfoot.appendChild(warn);
    return tfoot;
  }

  // --- interaction ---------------------------------------------------------

  function onInput(e) {
    var input = e.target;
    var line = lineFor(current, input.dataset.slug);
    var raw = input.value.trim();
    if (raw === '') delete line[input.dataset.col];
    else line[input.dataset.col] = parseInt(raw, 10) || 0;

    saveAll(sheets);
    updateRow(input.closest('tr'));
    updateTotals();
    updateState();
  }

  function onKeyDown(e) {
    var move = 0;
    if (e.key === 'ArrowDown' || e.key === 'Enter') move = 1;
    else if (e.key === 'ArrowUp') move = -1;
    else return;
    e.preventDefault();

    var row = parseInt(e.target.dataset.row, 10) + move;
    var col = e.target.dataset.colIdx;
    var next = root.querySelector('input[data-row="' + row + '"][data-col-idx="' + col + '"]');
    if (next) next.focus();
  }

  function updateRow(tr) {
    var line = lineFor(current, tr.dataset.slug);
    var bad = problems(line);

    tr.querySelectorAll('input').forEach(function (input) {
      var msg = bad[input.dataset.col];
      input.classList.toggle('outline', !!msg);
      input.classList.toggle('outline-2', !!msg);
      input.classList.toggle('outline-red-500', !!msg);
      input.title = msg || '';
    });

    var d = derived(line);
    var empty = isEmpty(line);
    tr.querySelector('[data-rate="avg"]').textContent = empty ? '—' : fmtRate(d.avg);
    tr.querySelector('[data-rate="obp"]').textContent = empty ? '—' : fmtRate(d.obp);
    tr.querySelector('[data-rate="slg"]').textContent = empty ? '—' : fmtRate(d.slg);
    tr.querySelector('[data-rate="rp7"]').textContent = empty ? '—' : fmtCount(d.rp7);
  }

  function updateTotals() {
    var sheet = sheetFor(current);
    var totals = {};
    COLS.forEach(function (c) { totals[c] = 0; });
    Object.keys(sheet).forEach(function (slug) {
      COLS.forEach(function (c) { totals[c] += n(sheet[slug], c); });
    });

    COLS.forEach(function (c) {
      var cell = root.querySelector('[data-total="' + c + '"]');
      if (cell) cell.textContent = totals[c];
    });

    // The single best catch for a mistyped line: runs scored by the lineup have
    // to equal the team's runs in the final score.
    var box = root.querySelector('#sheet-warning td');
    if (!box) return;
    var anyData = Object.keys(sheet).some(function (s) { return !isEmpty(sheet[s]); });
    if (current.played && anyData && totals.r !== current.us) {
      box.className = 'px-2 py-2 text-left text-sm text-red-400';
      box.textContent = 'Runs entered (' + totals.r + ') do not match the final score (' +
        current.us + '). Check for a missed or doubled line.';
    } else if (anyData) {
      box.className = 'px-2 py-2 text-left text-sm text-gray-500';
      box.textContent = current.played
        ? 'Runs match the final score.'
        : 'This game has no recorded score to check against.';
    } else {
      box.textContent = '';
    }
  }

  // --- actions -------------------------------------------------------------

  function actions() {
    var wrap = el('div', 'flex flex-wrap items-center gap-3 pt-2');

    var saveBtn = button('Save', 'bg-white text-black', save);
    saveBtn.id = 'sheet-save';
    wrap.appendChild(saveBtn);

    wrap.appendChild(button('Clear this game', '', function () {
      if (!confirm('Clear every line for ' + current.label + ' vs ' + current.opponent +
                   '?\n\nNothing is removed from the site until you save.')) return;
      sheets[gameKey(current)] = {};
      saveAll(sheets);
      render();
      updateTotals();
    }));

    var state = el('span', 'text-xs uppercase tracking-widest');
    state.id = 'sheet-state';
    wrap.appendChild(state);

    var done = gamesWithData().length;
    wrap.appendChild(el('span', 'text-xs uppercase tracking-widest text-gray-500',
      done + ' of ' + data.games.length + ' games typed'));

    var msg = el('span', 'text-sm');
    msg.id = 'sheet-flash';
    wrap.appendChild(msg);
    return wrap;
  }

  // updateState says whether the grid matches the database. It is the only
  // warning there is that closing the tab would lose something.
  function updateState() {
    var box = document.getElementById('sheet-state');
    if (!box || !current) return;
    var unsaved = dirty(current);
    box.className = 'text-xs uppercase tracking-widest ' + (unsaved ? 'text-yellow-400' : 'text-gray-500');
    box.textContent = unsaved ? 'Unsaved changes' : 'Saved';
  }

  // save writes the current game to the site, replacing whatever was stored for
  // it. Lines that fail validation are refused here rather than server-side so
  // the offending cell is already outlined in front of you.
  function save() {
    var sheet = sheetFor(current);
    var bad = [];
    var lines = {};
    data.roster.forEach(function (p) {
      var line = sheet[p.slug];
      if (!line || (isEmpty(line) && n(line, 'rbi') === 0)) return;
      if (Object.keys(problems(line)).length) { bad.push(displayName(p)); return; }
      var out = {};
      COLS.forEach(function (c) { out[c] = n(line, c); });
      lines[p.slug] = out;
    });
    if (bad.length) {
      flash('Fix the outlined cells first: ' + bad.join(', ') + '.', true);
      return;
    }

    var btn = document.getElementById('sheet-save');
    if (btn) { btn.disabled = true; btn.textContent = 'Saving...'; }

    fetch('/statsheet/save', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ game_id: current.id, lines: lines })
    }).then(function (res) {
      return res.json().catch(function () { return { ok: false, message: 'Save failed (' + res.status + ').' }; });
    }).then(function (body) {
      if (!body.ok) { flash(body.message || 'Save failed.', true); return; }
      // The server is now the truth for this game, so the draft and the
      // server copy are re-synced together and the grid reads clean. Re-render
      // so the picker's ticks and the typed-games count catch up too.
      current.lines = lines;
      sheets[gameKey(current)] = normalise(lines);
      saveAll(sheets);
      render();
      updateTotals();
      flash(body.message, false);
    }).catch(function () {
      flash('Could not reach the site. Your typing is still here.', true);
    }).then(function () {
      // render() replaces the button, so find whichever one is on screen now.
      var b = document.getElementById('sheet-save');
      if (b) { b.disabled = false; b.textContent = 'Save'; }
    });
  }

  function flash(msg, isError) {
    var box = document.getElementById('sheet-flash');
    if (!box) return;
    box.className = 'text-sm ' + (isError ? 'text-red-400' : 'text-gray-400');
    box.textContent = msg;
    setTimeout(function () { if (box.textContent === msg) box.textContent = ''; }, 4000);
  }

  // --- tiny DOM helpers ----------------------------------------------------

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    e.className = cls || '';
    if (text !== undefined) e.textContent = text;
    return e;
  }
  function th(text, extra) {
    var e = document.createElement('th');
    e.className = 'border border-white px-2 py-2 ' + (extra || '');
    e.textContent = text;
    return e;
  }
  function td(text, cls) {
    var e = document.createElement('td');
    e.className = cls || '';
    e.textContent = text;
    return e;
  }
  function button(label, extra, onClick) {
    var b = document.createElement('button');
    b.type = 'button';
    b.className = 'border border-white px-4 py-2 text-sm font-bold uppercase tracking-wide hover:bg-white hover:text-black ' + extra;
    b.textContent = label;
    b.addEventListener('click', onClick);
    return b;
  }

  render();
  updateTotals();
})();
