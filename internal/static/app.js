(function () {
  const $ = (sel, root) => (root || document).querySelector(sel);
  const BASE = document.body.dataset.base || '';

  async function api(method, path, body) {
    const res = await fetch(BASE + '/api/ui' + path, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await res.text();
    let data = {};
    try { data = text ? JSON.parse(text) : {}; } catch (e) { data = { error: text }; }
    if (!res.ok) throw new Error(data.error || (res.status + ' ' + res.statusText));
    return data;
  }

  function setMsg(el, text, kind) {
    if (!el) return;
    el.textContent = text;
    el.className = 'msg' + (kind ? ' ' + kind : '');
  }

  // Players page: health details, headset check, copy, reindex.
  const toggle = $('#health-toggle');
  const detail = $('#health-detail');
  if (toggle && detail) {
    toggle.addEventListener('click', () => {
      const open = detail.hidden;
      detail.hidden = !open;
      toggle.setAttribute('aria-expanded', String(open));
      toggle.querySelector('.more').textContent = open ? 'Hide details' : 'Details';
    });
  }
  const headset = $('#headset-check');
  if (headset && headset.dataset.cover) {
    const img = new Image();
    img.onload = () => { headset.textContent = 'Yes, this device can load covers from Stash.'; };
    img.onerror = () => { headset.textContent = 'No. This device cannot load images from Stash. Check that it can reach the Stash host, and that Stash uses https if this page does.'; };
    img.src = headset.dataset.cover;
  }
  document.querySelectorAll('[data-copy]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      try { await navigator.clipboard.writeText(btn.dataset.copy); btn.textContent = 'Copied'; }
      catch (e) { btn.textContent = 'Select the address'; }
      setTimeout(() => { btn.textContent = 'Copy'; }, 1500);
    });
  });
  const reindex = $('#reindex');
  if (reindex) {
    reindex.addEventListener('click', async () => {
      reindex.disabled = true;
      setMsg($('#reindex-msg'), 'Rebuilding');
      try {
        const r = await api('POST', '/reindex');
        setMsg($('#reindex-msg'), 'Rebuilt: ' + r.sections + ' sections, ' + r.scenes + ' scenes', 'ok');
      } catch (e) { setMsg($('#reindex-msg'), e.message, 'err'); }
      reindex.disabled = false;
    });
  }

  // Setup form.
  const form = $('#setup');
  if (form) {
    const read = () => ({
      stash_graphql_url: form.stash_graphql_url.value.trim(),
      stash_api_key: form.stash_api_key.value,
      favorite_tag: form.favorite_tag.value.trim(),
      exclude_sort_name: form.exclude_sort_name.value.trim(),
      generate_summary_ids: form.generate_summary_ids.checked,
      heatmap_height_px: Number(form.heatmap_height_px.value || 0),
      force_https: form.force_https.checked,
      deovr_autoload: form.deovr_autoload.checked,
      funscript_index_path: form.funscript_index_path.value.trim(),
      base_path: form.base_path.value.trim(),
      log_level: form.log_level.value,
      smart_section_size: Number(form.smart_section_size.value || 50),
    });
    $('#test').addEventListener('click', async () => {
      setMsg($('#test-msg'), 'Testing');
      try {
        const r = await api('POST', '/config/test', { stash_graphql_url: form.stash_graphql_url.value.trim(), stash_api_key: form.stash_api_key.value });
        setMsg($('#test-msg'), r.ok ? 'Connected to Stash ' + r.stash_version : r.error, r.ok ? 'ok' : 'err');
      } catch (e) { setMsg($('#test-msg'), e.message, 'err'); }
    });
    form.addEventListener('submit', async (ev) => {
      ev.preventDefault();
      setMsg($('#save-msg'), 'Saving');
      try {
        const cfg = await api('PUT', '/config', read());
        form.stash_api_key.value = '';
        form.stash_api_key.placeholder = cfg.stash_api_key_set ? 'set, leave blank to keep' : 'paste the key from Stash, Settings, Security';
        setMsg($('#save-msg'), 'Saved and applied', 'ok');
      } catch (e) { setMsg($('#save-msg'), e.message, 'err'); }
    });
  }

  // makeSortable lets the rows of tbody be reordered by dragging their
  // .handle, with mouse or touch.
  function makeSortable(tbody) {
    let dragRow = null;
    tbody.addEventListener('dragstart', (e) => {
      const handle = e.target.closest('.handle');
      if (!handle) return;
      dragRow = handle.closest('tr, .rule');
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', '');
      dragRow.classList.add('dragging');
    });
    tbody.addEventListener('dragend', () => { if (dragRow) dragRow.classList.remove('dragging'); dragRow = null; });
    tbody.addEventListener('dragover', (e) => {
      if (!dragRow) return;
      e.preventDefault();
      const over = e.target.closest('tr, .rule');
      if (!over || over === dragRow) return;
      const rect = over.getBoundingClientRect();
      tbody.insertBefore(dragRow, (e.clientY - rect.top) < rect.height / 2 ? over : over.nextSibling);
    });
    tbody.addEventListener('drop', (e) => e.preventDefault());
    let touchRow = null;
    tbody.addEventListener('touchstart', (e) => {
      const handle = e.target.closest('.handle');
      if (!handle) return;
      touchRow = handle.closest('tr, .rule');
      touchRow.classList.add('dragging');
      e.preventDefault();
    }, { passive: false });
    tbody.addEventListener('touchmove', (e) => {
      if (!touchRow) return;
      const t = e.touches[0];
      const el = document.elementFromPoint(t.clientX, t.clientY);
      const over = el && el.closest('tr, .rule');
      if (over && over !== touchRow) {
        const rect = over.getBoundingClientRect();
        tbody.insertBefore(touchRow, (t.clientY - rect.top) < rect.height / 2 ? over : over.nextSibling);
      }
      e.preventDefault();
    }, { passive: false });
    const endTouch = () => { if (touchRow) touchRow.classList.remove('dragging'); touchRow = null; };
    tbody.addEventListener('touchend', endTouch);
    tbody.addEventListener('touchcancel', endTouch);
  }

  // Setup page: video rules table.
  const rulesBody = $('#rules');
  if (rulesBody) {
    makeSortable(rulesBody);
    const ruleRows = () => Array.from(rulesBody.querySelectorAll('[data-rule]')).map((tr) => ({
      tag: tr.querySelector('.tag').value.trim(),
      projection: tr.querySelector('.projection').value,
      stereo: tr.querySelector('.stereo').value,
      fov: Number(tr.querySelector('.fov').value || 0),
      lens: tr.querySelector('.lens').value,
      passthrough: tr.querySelector('.passthrough').checked,
      profile: tr.querySelector('.profile').value,
    }));
    rulesBody.addEventListener('click', (e) => {
      const btn = e.target.closest('.remove');
      if (btn) btn.closest('[data-rule]').remove();
    });
    $('#add-rule').addEventListener('click', () => {
      rulesBody.appendChild($('#rule-template').content.firstElementChild.cloneNode(true));
    });
    $('#save-rules').addEventListener('click', async () => {
      setMsg($('#rules-msg'), 'Saving');
      try { await api('PUT', '/video-rules', ruleRows()); setMsg($('#rules-msg'), 'Saved. Scenes use the new rules when opened next.', 'ok'); }
      catch (e) { setMsg($('#rules-msg'), e.message, 'err'); }
    });
    $('#reset-rules').addEventListener('click', async () => {
      setMsg($('#rules-msg'), 'Resetting');
      try { await api('PUT', '/video-rules', []); location.reload(); }
      catch (e) { setMsg($('#rules-msg'), e.message, 'err'); }
    });
  }

  // Sections page: drag to reorder, save, reset.
  const tbody = $('#rows');
  if (tbody) {
    makeSortable(tbody);
    const rows = () => Array.from(tbody.querySelectorAll('tr')).map((tr) => ({
      id: tr.dataset.id,
      name: tr.querySelector('.name').value.trim(),
      disabled: !tr.querySelector('.enabled').checked,
    }));
    $('#save-filters').addEventListener('click', async () => {
      setMsg($('#filters-msg'), 'Saving');
      try { await api('PUT', '/filters', rows()); setMsg($('#filters-msg'), 'Saved. Players pick it up on their next index load.', 'ok'); }
      catch (e) { setMsg($('#filters-msg'), e.message, 'err'); }
    });
    $('#reset-filters').addEventListener('click', async () => {
      setMsg($('#filters-msg'), 'Resetting');
      try { await api('PUT', '/filters', []); location.reload(); }
      catch (e) { setMsg($('#filters-msg'), e.message, 'err'); }
    });
  }
})();
