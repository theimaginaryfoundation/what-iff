/**
 * What If's bridge into the draw.io editor (ADR 0x049): comments on a diagram's elements.
 *
 * Appended to js/PostConfig.js by scripts/build-diagrams-site.sh, it runs as a draw.io plugin
 * (Draw.loadPlugin) in the embedded editor and talks to the app with the embed protocol's JSON
 * messages, under names of its own (`whatiff-…`):
 *
 * - to the app: `whatiff-page` (the page shown), `whatiff-selection` (the selected elements, with
 *   their labels), `whatiff-click` (an element, or its comment mark, clicked), `whatiff-menu` (a right-click item),
 *   `whatiff-imported` / `whatiff-import-failed` (a Visio file converted, or not);
 * - from the app: `whatiff-marks` (which elements have threads, and which is active),
 *   `whatiff-reveal` (select and show an element), `whatiff-import-visio` (convert a .vsdx).
 *
 * It uses draw.io's own graph API only (selection, overlays, popup menu), so it stays small and is
 * checked again whenever the pinned release changes.
 */
(function () {
  if (typeof App === 'undefined' || !App.initPluginCallback) return;
  // PostConfig.js runs before App.main creates the plugin hook (window.Draw), so it is created
  // here; App.main's own call keeps it. Counting this plugin makes embed mode send `init` only
  // once it has run, as for the plugins draw.io loads itself.
  App.initPluginCallback();
  App.embedModePluginsCount = (App.embedModePluginsCount || 0) + 1;
  Draw.loadPlugin(function (ui) {
    var graph = ui.editor.graph;
    /** Thread state by element id on the page shown: 'open' or 'active'. */
    var marks = {};
    var overlays = {};

    function post(message) {
      // As draw.io's own embed messages: the page that embeds the editor is its only reader, and
      // only the app's origin may frame it (frame-ancestors).
      window.parent.postMessage(JSON.stringify(message), '*');
    }

    function pageId() {
      return ui.currentPage != null ? ui.currentPage.getId() : null;
    }

    function labelOf(cell) {
      var value = graph.convertValueToString(cell) || '';
      var div = document.createElement('div');
      div.innerHTML = value;
      return (div.textContent || div.innerText || '').replace(/\s+/g, ' ').trim().slice(0, 200);
    }

    function bubble(color) {
      var svg =
        '<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 18 18">' +
        '<path d="M3 2h12a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2H8l-4 3v-3H3a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2z" fill="' +
        color +
        '" stroke="#ffffff" stroke-width="1"/></svg>';
      return new mxImage('data:image/svg+xml;base64,' + btoa(svg), 18, 18);
    }

    var OPEN = bubble('#FFBD37');
    var ACTIVE = bubble('#8C87FF');

    function clearOverlays() {
      for (var id in overlays) {
        var cell = graph.model.getCell(id);
        if (cell != null) graph.removeCellOverlay(cell, overlays[id]);
      }
      overlays = {};
    }

    function applyMarks() {
      clearOverlays();
      for (var id in marks) {
        var cell = graph.model.getCell(id);
        if (cell == null) continue;
        var overlay = new mxCellOverlay(marks[id] === 'active' ? ACTIVE : OPEN, 'Comments', mxConstants.ALIGN_RIGHT, mxConstants.ALIGN_TOP);
        overlay.cursor = 'pointer';
        (function (cellId) {
          overlay.addListener(mxEvent.CLICK, function () {
            var cell = graph.model.getCell(cellId);
            post({ event: 'whatiff-click', cell: cellId, page: pageId(), label: cell != null ? labelOf(cell) : '' });
          });
        })(id);
        graph.addCellOverlay(cell, overlay);
        overlays[id] = overlay;
      }
    }

    function selection() {
      var cells = graph.getSelectionCells();
      var out = [];
      for (var i = 0; i < cells.length; i++) out.push({ id: cells[i].id, label: labelOf(cells[i]) });
      return out;
    }

    graph.getSelectionModel().addListener(mxEvent.CHANGE, function () {
      post({ event: 'whatiff-selection', page: pageId(), cells: selection() });
    });

    // Any element clicked, as a viewer (no menus, no selection) has nothing else to choose one with.
    function clicked(cell) {
      if (cell != null && cell.id !== '0' && cell.id !== '1') {
        post({ event: 'whatiff-click', cell: cell.id, page: pageId(), label: labelOf(cell) });
      }
    }
    graph.addListener(mxEvent.CLICK, function (sender, evt) {
      if (graph.isEnabled()) clicked(evt.getProperty('cell'));
    });
    // Viewing (chrome=0) the graph is disabled and fires no CLICK: a press and release on one
    // element, without a drag, is the click.
    var pressed = null;
    graph.addMouseListener({
      mouseDown: function (sender, me) {
        pressed = graph.isEnabled() ? null : { cell: me.getCell(), x: me.getX(), y: me.getY() };
      },
      mouseMove: function () {},
      mouseUp: function (sender, me) {
        var down = pressed;
        pressed = null;
        if (down == null || graph.isEnabled() || me.getCell() !== down.cell) return;
        if (Math.abs(me.getX() - down.x) + Math.abs(me.getY() - down.y) > 6) return;
        clicked(me.getCell());
      },
    });

    ui.editor.addListener('pageSelected', function () {
      marks = {};
      clearOverlays();
      post({ event: 'whatiff-page', page: pageId() });
    });

    // A diagram loaded or changed: overlays belong to cells, so they are put back.
    graph.model.addListener(mxEvent.CHANGE, function () {
      applyMarks();
    });

    // Right-click items, as every other format has them (ADR 0x045).
    var createPopupMenu = ui.menus.createPopupMenu;
    ui.menus.createPopupMenu = function (menu, cell, evt) {
      createPopupMenu.apply(this, arguments);
      var target = graph.getSelectionCount() === 1 ? graph.getSelectionCell() : null;
      if (target == null) return;
      menu.addSeparator();
      var marked = !!marks[target.id];
      menu.addItem(marked ? 'Reply to comment' : 'New comment', null, function () {
        post({ event: 'whatiff-menu', item: marked ? 'reply' : 'new', cell: target.id, page: pageId(), label: labelOf(target) });
      });
      if (marked) {
        menu.addItem('Delete comment', null, function () {
          post({ event: 'whatiff-menu', item: 'delete', cell: target.id, page: pageId(), label: labelOf(target) });
        });
      }
    };

    window.addEventListener('message', function (event) {
      if (event.source !== window.parent || typeof event.data !== 'string') return;
      var message;
      try {
        message = JSON.parse(event.data);
      } catch (e) {
        return;
      }
      if (message == null) return;
      if (message.action === 'whatiff-marks') {
        marks = message.page == null || message.page === pageId() ? message.marks || {} : {};
        applyMarks();
      } else if (message.action === 'whatiff-reveal') {
        if (message.page != null && message.page !== pageId() && ui.pages != null) {
          for (var i = 0; i < ui.pages.length; i++) {
            if (ui.pages[i].getId() === message.page) {
              ui.selectPage(ui.pages[i]);
              break;
            }
          }
        }
        var cell = graph.model.getCell(message.cell);
        if (cell != null) {
          graph.setSelectionCell(cell);
          graph.scrollCellToVisible(cell, true);
        }
      } else if (message.action === 'whatiff-import-visio') {
        importVisio(message.data, message.name);
      }
    });

    // A Visio file (.vsdx, as base64) converted with draw.io's own importer, which is in
    // extensions.min.js: offline, draw.io never loads that file itself, so it is loaded here.
    function importVisio(data, name) {
      var failed = function (error) {
        post({ event: 'whatiff-import-failed', message: (error && error.message) || String(error || 'unknown error') });
      };
      var run = function () {
        try {
          var bin = atob(data || '');
          var bytes = new Uint8Array(bin.length);
          for (var i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
          var blob = new Blob([bytes], { type: 'application/vnd.ms-visio.drawing' });
          ui.importVisio(
            blob,
            function (xml) {
              post({ event: 'whatiff-imported', xml: xml });
            },
            failed,
            name || 'diagram.vsdx',
          );
        } catch (e) {
          failed(e);
        }
      };
      if (ui.doImportVisio) run();
      else mxscript('js/extensions.min.js', run, null, null, null, failed);
    }

    post({ event: 'whatiff-page', page: pageId() });
  });
})();
