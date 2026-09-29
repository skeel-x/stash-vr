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

  // confirmClick makes btn a two-step button: the first click arms it and
  // shows its data-confirm text in place; a second click within a few
  // seconds runs action, else the button disarms by itself. There is no
  // dialog to dismiss, which matters in a headset browser.
  const confirmArmMs = 6000;
  function confirmClick(btn, action) {
    const label = btn.textContent;
    let timer = null;
    const disarm = () => { clearTimeout(timer); timer = null; btn.textContent = label; btn.classList.remove('armed'); };
    btn.addEventListener('click', () => {
      if (timer === null) {
        btn.textContent = btn.dataset.confirm || 'Click again to confirm';
        btn.classList.add('armed');
        timer = setTimeout(disarm, confirmArmMs);
        return;
      }
      disarm();
      action();
    });
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
  // copyText puts text on the clipboard and reports whether it got there.
  // navigator.clipboard only exists on a secure origin (https, or
  // localhost); over plain http, which is how most headsets open this
  // page, a hidden textarea and execCommand("copy") do the same.
  async function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) {
      try { await navigator.clipboard.writeText(text); return true; } catch (e) { /* try the fallback */ }
    }
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.setAttribute('aria-hidden', 'true');
    ta.style.position = 'fixed'; ta.style.top = '0'; ta.style.left = '0'; ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    ta.setSelectionRange(0, text.length);
    let ok = false;
    try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
    ta.remove();
    return ok;
  }
  document.querySelectorAll('[data-copy]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      btn.textContent = (await copyText(btn.dataset.copy)) ? 'Copied' : 'Select the address';
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

  // Players page: one random scene under Details.
  const randomItem = $('#random-item');
  if (randomItem) {
    const shuffle = $('#shuffle');
    shuffle.addEventListener('click', async () => {
      shuffle.disabled = true;
      try {
        const r = await api('GET', '/random?n=1');
        const s = r.scenes[0];
        if (s) {
          const a = document.createElement('a'); a.className = 'random-card'; a.href = s.stash; a.target = '_blank'; a.rel = 'noopener';
          const img = document.createElement('img'); img.src = s.cover; img.alt = ''; img.loading = 'lazy';
          const t = document.createElement('span'); t.textContent = s.title;
          a.append(img, t);
          randomItem.querySelectorAll('.random-card').forEach((el) => el.remove());
          randomItem.prepend(a);
        }
      } catch (e) { /* keep the current scene */ }
      shuffle.disabled = false;
    });
  }

  // Setup page: the rules table's readers, set up below with the table;
  // the settings form saves the rules along with everything else.
  let ruleRows = null;
  let showDefaultRules = null;
  // Setup page: unsaved edits, kept apart so "Save rules" clears only the
  // table's; leaving the page with either set asks first.
  let settingsDirty = false;
  let rulesDirty = false;
  const markDirty = (target) => {
    // The inspector, the badge preview and the coverage check are tools
    // inside the form, not settings.
    if (target.closest('#inspect-q, #badge-preview-scene, #coverage-panel, #profile-select')) return;
    if (target.closest('#rules, #add-preset')) rulesDirty = true;
    else settingsDirty = true;
  };

  // Setup form.
  const form = $('#setup');
  if (form) {
    form.addEventListener('input', (e) => markDirty(e.target));
    form.addEventListener('change', (e) => markDirty(e.target));
    window.addEventListener('beforeunload', (e) => {
      if (!settingsDirty && !rulesDirty) return;
      e.preventDefault();
      e.returnValue = '';
    });
    // The key is left out to keep the stored one, sent empty to clear it
    // ("No API key" ticked), else sent as typed.
    const apiKey = () => (form.stash_no_api_key.checked ? '' : (form.stash_api_key.value.trim() || undefined));
    form.stash_api_key.addEventListener('input', () => { if (form.stash_api_key.value.trim()) form.stash_no_api_key.checked = false; });
    form.stash_no_api_key.addEventListener('change', () => { if (form.stash_no_api_key.checked) form.stash_api_key.value = ''; });
    // The rules table is sent along when the page has one, so "Save
    // changes" saves everything on the page.
    const read = () => ({
      video_rules: ruleRows ? ruleRows() : undefined,
      stash_graphql_url: form.stash_graphql_url.value.trim(),
      stash_api_key: apiKey(),
      stash_tls_insecure: form.stash_tls_insecure.checked,
      favorite_tag: form.favorite_tag.value.trim(),
      exclude_sort_name: form.exclude_sort_name.value.trim(),
      generate_summary_ids: form.generate_summary_ids.checked,
      heatmap_height_px: Number(form.heatmap_height_px.value || 0),
      force_https: form.force_https.checked,
      deovr_autoload: form.deovr_autoload.checked,
      performer_facets: form.performer_facets.checked,
      date_lookup: form.date_lookup.checked,
      date_writeback: form.date_writeback.checked,
      funscript_index_path: form.funscript_index_path.value.trim(),
      base_path: form.base_path.value.trim(),
      log_level: form.log_level.value,
      smart_section_size: Number(form.smart_section_size.value || 50),
      auto_studio_min: Number(form.auto_studio_min.value || 0),
      auto_performer_min: Number(form.auto_performer_min.value || 0),
      learn_studio_profiles: form.learn_studio_profiles.checked,
      correct_vertical_stereo: form.correct_vertical_stereo.checked,
      cover_badges: {
        quality: form.cover_badge_quality.checked,
        format: form.cover_badge_format.checked,
        passthrough: form.cover_badge_passthrough.checked,
        duration: form.cover_badge_duration.checked,
        framerate: form.cover_badge_framerate.checked,
      },
    });
    $('#test').addEventListener('click', async () => {
      setMsg($('#test-msg'), 'Testing');
      try {
        const r = await api('POST', '/config/test', { stash_graphql_url: form.stash_graphql_url.value.trim(), stash_api_key: apiKey(), stash_tls_insecure: form.stash_tls_insecure.checked });
        // The server appends /graphql to a bare host; show what it tested.
        if (r.stash_graphql_url) form.stash_graphql_url.value = r.stash_graphql_url;
        setMsg($('#test-msg'), r.ok ? 'Connected to Stash ' + r.stash_version : r.error, r.ok ? 'ok' : 'err');
      } catch (e) { setMsg($('#test-msg'), e.message, 'err'); }
    });
    form.addEventListener('submit', async (ev) => {
      ev.preventDefault();
      setMsg($('#save-msg'), 'Saving');
      const body = read();
      try {
        const cfg = await api('PUT', '/config', body);
        form.stash_graphql_url.value = cfg.stash_graphql_url;
        form.stash_api_key.value = '';
        form.stash_api_key.placeholder = cfg.stash_api_key_set ? 'set, leave blank to keep' : 'paste the key from Stash, Settings, Security';
        form.stash_no_api_key.checked = !cfg.stash_api_key_set;
        settingsDirty = false;
        rulesDirty = false;
        if (body.video_rules && !body.video_rules.length) {
          showDefaultRules();
          setMsg($('#save-msg'), 'Saved and applied. The rules table was empty, so the default rules were restored.', 'ok');
        } else setMsg($('#save-msg'), 'Saved and applied', 'ok');
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
    // Enter in a rule input would submit the settings form around the
    // table (its implicit submission), saving everything mid-edit.
    rulesBody.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && e.target.matches('input')) e.preventDefault();
    });
    // Reordering, adding and removing rules change the table without an
    // input event.
    rulesBody.addEventListener('dragend', () => { rulesDirty = true; });
    rulesBody.addEventListener('touchend', () => { rulesDirty = true; });
    // Blank screen fields are left out, so the rule keeps them unset.
    ruleRows = () => Array.from(rulesBody.querySelectorAll('[data-rule]')).map((tr) => {
      const rule = {
        tag: tr.querySelector('.tag').value.trim(),
        projection: tr.querySelector('.projection').value,
        stereo: tr.querySelector('.stereo').value,
        fov: Number(tr.querySelector('.fov').value || 0),
        lens: tr.querySelector('.lens').value,
        passthrough: tr.querySelector('.passthrough').checked,
        profile: tr.querySelector('.profile').value,
      };
      tr.querySelectorAll('.geom').forEach((input) => {
        const v = input.value.trim();
        if (v !== '') rule[input.dataset.key] = Number(v);
      });
      const background = tr.querySelector('.background').value;
      if (background) rule.background = background;
      if (background === 'color') rule.background_color = tr.querySelector('.background-color').value;
      const mask = tr.querySelector('.mask').value;
      if (mask) rule.mask = mask;
      // Eye swap and force mono are left out when unchanged.
      [['.eye-swap', 'eye_swap'], ['.force-mono', 'force_mono']].forEach(([sel, key]) => {
        const v = tr.querySelector(sel).value;
        if (v) rule[key] = v === 'on';
      });
      return rule;
    });
    // "Copy from a saved profile" fills the rule's screen fields from the
    // decoded profile; nothing is saved until "Save rules".
    rulesBody.addEventListener('change', async (e) => {
      const select = e.target.closest('.copy-profile');
      if (!select || !select.value) return;
      const rule = select.closest('[data-rule]');
      const msg = rule.querySelector('.copy-row .msg');
      setMsg(msg, 'Loading');
      try {
        const p = await api('GET', '/profiles/' + encodeURIComponent(select.value));
        rule.querySelectorAll('.geom').forEach((input) => {
          const v = p[input.dataset.key];
          input.value = v === undefined || v === null ? '' : String(v);
        });
        rule.querySelector('.background').value = p.background || '';
        if (p.background_color) rule.querySelector('.background-color').value = p.background_color;
        rule.querySelector('.mask').value = p.mask || '';
        setMsg(msg, 'Copied from ' + (p.title || 'scene ' + p.id) + '. Save rules to keep it.', 'ok');
      } catch (err) { setMsg(msg, err.message, 'err'); }
      select.value = '';
    });
    rulesBody.addEventListener('click', (e) => {
      const btn = e.target.closest('.remove');
      if (btn) { btn.closest('[data-rule]').remove(); rulesDirty = true; }
    });
    $('#add-rule').addEventListener('click', () => {
      rulesBody.appendChild($('#rule-template').content.firstElementChild.cloneNode(true));
      rulesDirty = true;
    });
    // "Add a preset" appends a filled rule card to edit; nothing is saved
    // until "Save rules".
    $('#add-preset').addEventListener('change', (e) => {
      if (e.target.value === '') return;
      const card = $('#preset-rules').content.querySelectorAll('[data-rule]')[Number(e.target.value)];
      e.target.value = '';
      if (!card) return;
      const added = card.cloneNode(true);
      rulesBody.appendChild(added);
      added.querySelector('.tag').focus();
      rulesDirty = true;
      setMsg($('#rules-msg'), 'Preset added at the end. Adjust it, then save rules.', 'ok');
    });
    // Appends the default rules whose tag no rule in the table has yet
    // (compared case-insensitively, as tags match); existing rules keep
    // their order and values.
    $('#add-defaults').addEventListener('click', () => {
      const have = new Set(Array.from(rulesBody.querySelectorAll('[data-rule] .tag')).map((i) => i.value.trim().toLowerCase()));
      let added = 0;
      Array.from($('#default-rules').content.querySelectorAll('[data-rule]')).forEach((card) => {
        const tag = card.querySelector('.tag').value.trim().toLowerCase();
        if (have.has(tag)) return;
        rulesBody.appendChild(card.cloneNode(true));
        have.add(tag);
        added++;
      });
      if (added) rulesDirty = true;
      setMsg($('#rules-msg'), added ? 'Added ' + added + (added === 1 ? ' rule' : ' rules') + '. Save rules to keep them.' : 'Every default rule is already in the table.', 'ok');
    });
    // showDefaultRules replaces the table with the default rules, as the
    // server stores them when it is sent an empty list.
    showDefaultRules = () => {
      rulesBody.replaceChildren(...Array.from($('#default-rules').content.querySelectorAll('[data-rule]')).map((card) => card.cloneNode(true)));
    };
    $('#save-rules').addEventListener('click', async () => {
      const list = ruleRows();
      setMsg($('#rules-msg'), 'Saving');
      try {
        await api('PUT', '/video-rules', list);
        rulesDirty = false;
        if (!list.length) {
          showDefaultRules();
          setMsg($('#rules-msg'), 'Defaults restored: the table was empty, so the default rules were saved instead.', 'ok');
        } else setMsg($('#rules-msg'), 'Saved. Scenes use the new rules when opened next.', 'ok');
      } catch (e) { setMsg($('#rules-msg'), e.message, 'err'); }
    });
    // The table is redrawn in place rather than reloaded, so edits made
    // elsewhere on the page survive the reset.
    confirmClick($('#reset-rules'), async () => {
      setMsg($('#rules-msg'), 'Resetting');
      try {
        await api('PUT', '/video-rules', []);
        showDefaultRules();
        rulesDirty = false;
        setMsg($('#rules-msg'), 'Defaults restored.', 'ok');
      } catch (e) { setMsg($('#rules-msg'), e.message, 'err'); }
    });
  }

  // Setup page: delete a stored profile. The button is live once a profile
  // is picked and needs a second click; the deleted profile leaves the
  // copy-from lists, while a rule that names it keeps its choice (the
  // server ignores a profile that is gone).
  const deleteProfile = $('#delete-profile');
  if (deleteProfile) {
    const select = $('#profile-select');
    select.addEventListener('change', () => { deleteProfile.disabled = !select.value; });
    confirmClick(deleteProfile, async () => {
      const id = select.value;
      if (!id) return;
      deleteProfile.disabled = true;
      setMsg($('#profile-msg'), 'Deleting');
      try {
        const r = await api('DELETE', '/profiles/' + encodeURIComponent(id));
        document.querySelectorAll('#profile-select option, .copy-profile option').forEach((o) => { if (o.value === id) o.remove(); });
        select.value = '';
        $('#profile-count').textContent = String(r.profiles);
        setMsg($('#profile-msg'), 'Deleted the profile of scene ' + id + '. The file is kept in hsp/history.', 'ok');
      } catch (e) {
        setMsg($('#profile-msg'), e.message, 'err');
        deleteProfile.disabled = !select.value;
      }
    });
  }

  // Setup page: scene inspector. One request per click (or Enter), never
  // per keystroke.
  const inspectBtn = $('#inspect');
  if (inspectBtn) {
    const q = $('#inspect-q');
    const out = $('#inspect-out');
    const el = (tag, text, cls) => { const e = document.createElement(tag); if (text !== undefined) e.textContent = text; if (cls) e.className = cls; return e; };
    const describe = (f) => {
      const parts = [f.projection, f.stereo, f.lens, f.fov ? f.fov + '°' : '',
        f.passthrough ? 'passthrough' : '', f.eye_swap ? 'eye swap' : '', f.force_mono ? 'force mono' : '',
        f.background ? 'background ' + f.background : '', f.mask ? 'mask ' + f.mask : ''].filter(Boolean);
      if (f.geometry) parts.push('screen: ' + Object.keys(f.geometry).map((k) => k + ' ' + f.geometry[k]).join(', '));
      return parts.length ? parts.join(', ') : 'player defaults';
    };
    const sources = { own: 'own profile', rule: 'rule profile of scene ', generated: 'generated', none: 'none' };
    // The vertical stereo offset vrQualityTags measured and what became of it.
    const verticalReasons = {
      applied: 'corrected in the generated profile',
      profile_wins: 'not corrected: a saved profile is used instead',
      not_measured: 'not measured',
      invalid: 'vr_vertical_offset is not a number',
      off: 'not corrected: the setting is off',
      not_stereo: 'not corrected: not an SBS or TB scene',
      rule_pitch: 'not corrected: a rule sets the pitch',
      too_small: 'not corrected: below 0.25°',
      too_large: 'not corrected: above 1.5°, not trusted as a pure offset',
    };
    const vertical = (v) => {
      const reason = verticalReasons[v.reason] || v.reason;
      return v.offset === undefined ? reason : (v.offset > 0 ? '+' : '') + v.offset.toFixed(2) + '°, ' + reason;
    };
    const run = async () => {
      const query = q.value.trim();
      if (!query) { setMsg($('#inspect-msg'), 'Enter a scene id or part of a title', 'err'); return; }
      inspectBtn.disabled = true;
      setMsg($('#inspect-msg'), 'Looking up');
      try {
        const r = await api('GET', '/inspect?q=' + encodeURIComponent(query));
        out.replaceChildren();
        if (!r.scenes.length) {
          setMsg($('#inspect-msg'), 'No scene found. Titles only match scenes a player has loaded; try the id.', 'err');
        } else {
          setMsg($('#inspect-msg'), '', 'ok');
          const table = el('table', undefined, 'inspect');
          const head = table.createTHead().insertRow();
          ['Scene', 'Matching rules', 'Result', 'Profile', 'Vertical offset'].forEach((h) => head.appendChild(el('th', h)));
          const body = table.createTBody();
          r.scenes.forEach((s) => {
            const tr = body.insertRow();
            const scene = tr.insertCell();
            const a = el('a', s.id + ' ' + s.title); a.href = s.stash; a.target = '_blank'; a.rel = 'noopener';
            scene.appendChild(a);
            tr.insertCell().textContent = s.matched.length ? s.matched.map((m) => (m.index + 1) + '. ' + m.tag).join(', ') : 'none';
            tr.insertCell().textContent = describe(s.format);
            const prof = tr.insertCell();
            const label = sources[s.profile.source] || s.profile.source;
            if (s.profile.source === 'studio') prof.textContent = 'studio (from scene ' + s.profile.scene + ')';
            else prof.textContent = s.profile.source === 'rule' ? label + s.profile.scene : label;
            if (s.profile.link) {
              const l = el('a', 'link'); l.href = s.profile.link; l.target = '_blank'; l.rel = 'noopener';
              prof.append(' ', l);
            }
            tr.insertCell().textContent = vertical(s.vertical);
          });
          out.appendChild(table);
        }
      } catch (e) { setMsg($('#inspect-msg'), e.message, 'err'); }
      inspectBtn.disabled = false;
    };
    inspectBtn.addEventListener('click', run);
    // Enter would otherwise submit the settings form around it.
    q.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); run(); } });
  }

  // Setup page: cover badge preview. Redrawn on every badge checkbox
  // change with the boxes as ticked, before saving; an empty scene id
  // lets the server pick a scene that shows every badge kind.
  const previewImg = $('#badge-preview-img');
  if (previewImg) {
    const sceneInput = $('#badge-preview-scene');
    const previewMsg = $('#badge-preview-msg');
    const previewTitle = $('#badge-preview-title');
    const boxes = ['quality', 'format', 'passthrough', 'duration', 'framerate'].map((k) => [k, document.querySelector('input[name="cover_badge_' + k + '"]')]);
    let seq = 0;
    let objectUrl = '';
    const show = async () => {
      const mine = ++seq;
      const params = new URLSearchParams();
      const scene = sceneInput.value.trim();
      if (scene) params.set('scene', scene);
      boxes.forEach(([k, box]) => params.set(k, box && box.checked ? '1' : '0'));
      setMsg(previewMsg, 'Drawing');
      try {
        const res = await fetch(BASE + '/api/ui/badge-preview?' + params.toString());
        if (!res.ok) {
          let text = res.status + ' ' + res.statusText;
          try { text = (await res.json()).error || text; } catch (e) { /* not JSON */ }
          throw new Error(text);
        }
        const blob = await res.blob();
        if (mine !== seq) return;
        if (objectUrl) URL.revokeObjectURL(objectUrl);
        objectUrl = URL.createObjectURL(blob);
        previewImg.src = objectUrl;
        previewImg.hidden = false;
        const id = res.headers.get('X-Scene-Id') || '';
        let title = res.headers.get('X-Scene-Title') || '';
        try { title = decodeURIComponent(title); } catch (e) { /* keep as sent */ }
        previewTitle.textContent = id ? id + ' ' + title : title;
        setMsg(previewMsg, '');
      } catch (e) {
        if (mine !== seq) return;
        previewImg.hidden = true;
        previewTitle.textContent = '';
        setMsg(previewMsg, 'No preview: ' + e.message, 'err');
      }
    };
    boxes.forEach(([, box]) => { if (box) box.addEventListener('change', show); });
    $('#badge-preview-show').addEventListener('click', show);
    // Enter would otherwise submit the settings form around it.
    sceneInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); show(); } });
    show();
  }

  // Setup page: format coverage. Loaded only when the button is pressed so
  // the page never waits on Stash for it.
  const coverageBtn = $('#coverage');
  if (coverageBtn) {
    const out = $('#coverage-out');
    const msg = $('#coverage-msg');
    const cell = (tr, text, href, cls) => {
      const td = tr.insertCell();
      if (cls) td.className = cls;
      if (href) {
        const a = document.createElement('a'); a.href = href; a.target = '_blank'; a.rel = 'noopener'; a.textContent = text;
        td.appendChild(a);
      } else td.textContent = text;
      return td;
    };
    const table = (heads) => {
      const t = document.createElement('table'); t.className = 'inspect';
      const head = t.createTHead().insertRow();
      heads.forEach((h) => { const th = document.createElement('th'); th.textContent = h; head.appendChild(th); });
      return t;
    };
    coverageBtn.addEventListener('click', async () => {
      coverageBtn.disabled = true;
      setMsg(msg, 'Asking Stash');
      try {
        const r = await api('GET', '/coverage');
        out.replaceChildren();
        const tags = table(['Tag', 'Scenes']);
        const body = tags.createTBody();
        r.tags.forEach((t) => {
          const tr = body.insertRow();
          if (t.missing) tr.className = 'missing';
          cell(tr, t.name);
          cell(tr, t.missing ? 'no such tag' : String(t.count), t.link, 'num');
        });
        const tr = body.insertRow();
        cell(tr, 'VR-shaped scenes without a projection tag');
        cell(tr, String(r.untagged.count), undefined, 'num');
        out.appendChild(tags);
        if (r.untagged.scenes.length) {
          const list = table(['Untagged VR-shaped scene', 'Size']);
          const lb = list.createTBody();
          r.untagged.scenes.forEach((s) => {
            const row = lb.insertRow();
            cell(row, s.id + ' ' + s.title, s.stash);
            cell(row, s.width + 'x' + s.height, undefined, 'num');
          });
          out.appendChild(list);
        }
        const shown = r.untagged.scenes.length < r.untagged.count ? ' Showing the first ' + r.untagged.scenes.length + ' untagged scenes.' : '';
        setMsg(msg, 'Checked ' + new Date(r.checked_at).toLocaleTimeString() + '.' + shown, 'ok');
      } catch (e) { setMsg(msg, e.message, 'err'); }
      coverageBtn.disabled = false;
    });
  }

  // Sections page: drag to reorder, save, reset.
  const tbody = $('#rows');
  if (tbody) {
    makeSortable(tbody);
    const players = ['heresphere', 'deovr', 'playa'];
    // The first row shown in HereSphere is where the player lands; keep
    // the badge on it as rows are reordered or unticked.
    const markLanding = () => {
      let found = false;
      tbody.querySelectorAll('tr').forEach((tr) => {
        const first = !found && tr.querySelector('.show-heresphere').checked;
        tr.querySelector('.landing').hidden = !first;
        if (first) found = true;
      });
    };
    tbody.addEventListener('change', (e) => { if (e.target.classList.contains('show-heresphere')) markLanding(); });
    tbody.addEventListener('dragend', markLanding);
    tbody.addEventListener('touchend', markLanding);
    markLanding();
    const rows = () => Array.from(tbody.querySelectorAll('tr')).map((tr) => {
      const on = players.filter((p) => tr.querySelector('.show-' + p).checked);
      const off = players.filter((p) => !on.includes(p));
      return {
        id: tr.dataset.id,
        name: tr.querySelector('.name').value.trim(),
        disabled: on.length === 0,
        hidden_in: on.length === 0 ? [] : off,
      };
    });
    $('#save-filters').addEventListener('click', async () => {
      const list = rows();
      // An empty list would replace the stored order and names with
      // nothing; without rows there is nothing to save.
      if (!list.length) { setMsg($('#filters-msg'), 'Nothing to save: reload this page once Stash is reachable.', 'warn'); return; }
      setMsg($('#filters-msg'), 'Saving');
      try { await api('PUT', '/filters', list); setMsg($('#filters-msg'), 'Saved. Players pick it up on their next index load.', 'ok'); }
      catch (e) { setMsg($('#filters-msg'), e.message, 'err'); }
    });
    confirmClick($('#reset-filters'), async () => {
      setMsg($('#filters-msg'), 'Resetting');
      try { await api('PUT', '/filters', []); location.reload(); }
      catch (e) { setMsg($('#filters-msg'), e.message, 'err'); }
    });
  }

  // Log page: refresh and auto-refresh the tail, filtered by level and text.
  const logPre = $('#log-lines');
  if (logPre) {
    const levelSel = $('#log-level');
    const textIn = $('#log-filter');
    // The fetched (unfiltered) tail; the filter is applied on render so it survives refreshes.
    let lines = logPre.textContent.split('\n');
    if (lines.length && lines[lines.length - 1] === '') lines.pop();
    const rank = { debug: 0, info: 1, warn: 2, error: 3 };
    // Console lines carry one of these tokens; a line without one counts as info.
    const levelOf = (line) => {
      if (line.includes(' DBG ')) return 0;
      if (line.includes(' WRN ')) return 2;
      if (line.includes(' ERR ')) return 3;
      return 1;
    };
    const render = () => {
      const min = levelSel.value === 'all' ? 0 : rank[levelSel.value];
      const needle = textIn.value.trim().toLowerCase();
      const shown = lines.filter((l) => levelOf(l) >= min && (!needle || l.toLowerCase().includes(needle)));
      logPre.textContent = shown.length ? shown.join('\n') + '\n' : '';
      logPre.scrollTop = logPre.scrollHeight;
    };
    const load = async () => {
      try {
        const r = await api('GET', '/log?lines=300');
        lines = r.lines;
        render();
        setMsg($('#log-msg'), '', 'ok');
      } catch (e) { setMsg($('#log-msg'), e.message, 'err'); }
    };
    levelSel.addEventListener('change', render);
    textIn.addEventListener('input', render);
    $('#log-refresh').addEventListener('click', load);
    let timer = null;
    $('#log-auto').addEventListener('change', (e) => {
      if (e.target.checked) { load(); timer = setInterval(load, 5000); } else { clearInterval(timer); timer = null; }
    });
    render();
  }
})();
