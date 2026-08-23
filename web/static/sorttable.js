// Stats table behaviour: click-to-sort, plus a column-group toggle.
//
// The league's stat key defines 27 stats. Showing them all at once is
// unreadable, so every cell is tagged with a group and the toggle shows the
// standard set, the advanced set, or everything.
(function () {
  var table = document.getElementById('stats-table');
  if (!table) return;

  // --- column groups -------------------------------------------------------

  var MODES = {
    Standard: ['key', 'std'],
    Advanced: ['key', 'adv'],
    All: ['key', 'std', 'adv']
  };
  var STORE_KEY = 'coorsheavy:stats-columns';

  function applyMode(mode) {
    var groups = MODES[mode] || MODES.Standard;
    table.querySelectorAll('[data-group]').forEach(function (cell) {
      cell.classList.toggle('hidden', groups.indexOf(cell.dataset.group) === -1);
    });
    document.querySelectorAll('.stat-toggle').forEach(function (b) {
      var on = b.dataset.cols === mode;
      b.classList.toggle('bg-white', on);
      b.classList.toggle('text-black', on);
    });
    try { localStorage.setItem(STORE_KEY, mode); } catch (e) { /* private window */ }
  }

  document.querySelectorAll('.stat-toggle').forEach(function (b) {
    b.addEventListener('click', function () { applyMode(b.dataset.cols); });
  });

  var saved = null;
  try { saved = localStorage.getItem(STORE_KEY); } catch (e) { /* private window */ }
  applyMode(MODES[saved] ? saved : 'Standard');

  // --- click to sort -------------------------------------------------------

  var headers = table.tHead.rows[0].cells;
  var state = { index: -1, asc: false };

  function cellValue(row, i) {
    var text = (row.cells[i].textContent || '').trim();
    if (headers[i].dataset.sort === 'num') {
      // Rates render as ".412" with no leading zero, and an undefined rate as
      // an em dash — which must sort below every real value, not above it.
      var n = parseFloat(text.replace(/^\./, '0.').replace(/^-\./, '-0.'));
      return isNaN(n) ? -Infinity : n;
    }
    return text.toLowerCase();
  }

  function sortBy(i) {
    // First click on a new column sorts descending for numbers (best first)
    // and ascending for names.
    state.asc = state.index === i ? !state.asc : headers[i].dataset.sort !== 'num';
    state.index = i;

    var body = table.tBodies[0];
    var rows = Array.prototype.slice.call(body.rows);
    rows.sort(function (a, b) {
      var av = cellValue(a, i), bv = cellValue(b, i);
      if (av < bv) return state.asc ? -1 : 1;
      if (av > bv) return state.asc ? 1 : -1;
      return 0;
    });
    rows.forEach(function (r) { body.appendChild(r); });

    for (var h = 0; h < headers.length; h++) {
      headers[h].textContent = headers[h].textContent.replace(/ [▲▼]$/, '');
    }
    headers[i].textContent += state.asc ? ' ▲' : ' ▼';
  }

  for (var i = 0; i < headers.length; i++) {
    if (!headers[i].dataset.sort) continue;
    (function (idx) {
      headers[idx].addEventListener('click', function () { sortBy(idx); });
    })(i);
  }
})();
