// Tiny client-side table sort for the postmortems index.
//
// On click of a `.sortable-th`, sorts the parent table's <tbody> rows
// by the cell text (or `data-sort-value` attribute) of that column,
// toggling ascending / descending each click.
(function () {
  "use strict";

  function sortTable(table, columnIndex, ascending) {
    var tbody = table.tBodies[0];
    if (!tbody) return;
    var rows = Array.prototype.slice.call(tbody.rows);

    rows.sort(function (a, b) {
      var av = cellValue(a.cells[columnIndex]);
      var bv = cellValue(b.cells[columnIndex]);
      if (av < bv) return ascending ? -1 : 1;
      if (av > bv) return ascending ? 1 : -1;
      return 0;
    });

    var frag = document.createDocumentFragment();
    rows.forEach(function (row) {
      frag.appendChild(row);
    });
    tbody.appendChild(frag);
  }

  function cellValue(cell) {
    if (!cell) return "";
    if (cell.dataset && typeof cell.dataset.sortValue === "string") {
      return cell.dataset.sortValue.toLowerCase();
    }
    return (cell.textContent || "").trim().toLowerCase();
  }

  function clearArrows(table) {
    var headers = table.querySelectorAll("th .sort-arrow");
    headers.forEach(function (el) {
      el.textContent = "";
    });
  }

  function setArrow(th, ascending) {
    var span = th.querySelector(".sort-arrow");
    if (!span) return;
    span.textContent = ascending ? " \u25B2" : " \u25BC";
  }

  function init() {
    var tables = document.querySelectorAll("table.sortable");
    tables.forEach(function (table) {
      var headers = table.querySelectorAll("th.sortable-th");
      headers.forEach(function (th, idx) {
        th.style.cursor = "pointer";
        th.setAttribute("role", "button");
        th.setAttribute("tabindex", "0");
        // Index of this th amongst all ths so sortTable reads the
        // matching <td>. Allows non-sortable columns before it.
        var columnIndex = Array.prototype.indexOf.call(th.parentNode.cells, th);
        var ascending = true;
        var handler = function () {
          sortTable(table, columnIndex, ascending);
          clearArrows(table);
          setArrow(th, ascending);
          ascending = !ascending;
        };
        th.addEventListener("click", handler);
        th.addEventListener("keydown", function (ev) {
          if (ev.key === "Enter" || ev.key === " ") {
            ev.preventDefault();
            handler();
          }
        });
      });
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
