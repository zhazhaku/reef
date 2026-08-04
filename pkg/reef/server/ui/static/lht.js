/**
 * LHT 长程任务面板 — W5B UI Controller
 *
 * Dependencies: Reef global SSE EventBus (/api/v2/events)
 * REST API:  /api/v2/lht/list, /api/v2/lht/<id>, /api/v2/lht/<id>/logs,
 *            /api/v2/lht/<id>/pause|resume|stop|insert|approve|reject|reply
 */

const LHT = (function() {
  'use strict';

  // ─── State ────────────────────────────────────────────
  let currentGoalID = null;
  let tasks = [];
  let sseUnsub = null;

  // ─── State badge config ───────────────────────────────
  const STATE_BADGES = {
    GROUNDING:      { cls: 'badge-info',     icon: '🔍', label: '对齐中' },
    PLANNING:       { cls: 'badge-planning', icon: '📐', label: '规划中' },
    WAIT_APPROVAL:  { cls: 'badge-warning',  icon: '⏳', label: '待批准' },
    EXECUTING:      { cls: 'badge-active',   icon: '▶️',  label: '执行中' },
    EVALUATING:     { cls: 'badge-eval',     icon: '🔬', label: '评估中' },
    REVIEWING:      { cls: 'badge-review',   icon: '👁️',  label: '评审中' },
    FINAL_APPROVAL: { cls: 'badge-final',    icon: '🏁', label: '终审中' },
    COMPLETED:      { cls: 'badge-success',  icon: '✅', label: '已完成' },
    PAUSED:         { cls: 'badge-paused',   icon: '⏸️',  label: '已暂停' },
    ESCALATED:      { cls: 'badge-danger',   icon: '🚨', label: '已升级' },
    ABORTED:        { cls: 'badge-aborted',  icon: '🛑', label: '已终止' },
  };

  // ─── Routing ──────────────────────────────────────────
  function isActive() {
    return window.location.hash.startsWith('#/lht');
  }

  function parseHash() {
    const parts = window.location.hash.replace('#/', '').split('/');
    // parts[0] = 'lht', parts[1] = optional goalID
    if (parts.length >= 2 && parts[1]) {
      return parts[1]; // goalID
    }
    return null;
  }

  function show() {
    document.getElementById('lht-app').style.display = 'block';
    const goalID = parseHash();
    if (goalID) {
      showDetail(goalID);
    } else {
      showList();
    }
  }

  function hide() {
    document.getElementById('lht-app').style.display = 'none';
    if (sseUnsub) { sseUnsub(); sseUnsub = null; }
  }

  // ─── List Page ────────────────────────────────────────
  function showList() {
    document.getElementById('lht-list-page').style.display = 'block';
    document.getElementById('lht-detail-page').style.display = 'none';
    currentGoalID = null;
    fetchList();
  }

  async function fetchList() {
    try {
      const resp = await fetch('/api/v2/lht/list');
      if (!resp.ok) throw new Error('HTTP ' + resp.status);
      tasks = await resp.json();
      renderCards();
    } catch (err) {
      console.warn('[LHT] list fetch failed, using mock:', err.message);
      // Fallback: show empty state
      tasks = [];
      renderCards();
    }
  }

  function renderCards() {
    const container = document.getElementById('lht-task-cards');
    if (!tasks || tasks.length === 0) {
      container.innerHTML = '<div class="lht-empty">暂无长程任务。<br/>点击「新建任务」开始。</div>';
      return;
    }

    container.innerHTML = tasks.map(t => {
      const badge = STATE_BADGES[t.state] || { cls: 'badge-default', icon: '❓', label: t.state };
      const updated = t.updated_at ? new Date(t.updated_at).toLocaleString() : '—';
      const progress = computeProgress(t);
      return `
        <div class="lht-card" onclick="LHT.openDetail('${t.goal_id}')">
          <div class="lht-card-header">
            <span class="lht-card-title">${escHtml(t.description || t.goal_id)}</span>
            <span class="lht-badge ${badge.cls}">${badge.icon} ${badge.label}</span>
          </div>
          <div class="lht-card-body">
            <div class="lht-progress-bar">
              <div class="lht-progress-fill" style="width:${progress}%"></div>
            </div>
            <div class="lht-card-meta">
              <span>${progress}%</span>
              <span>更新: ${updated}</span>
            </div>
          </div>
        </div>
      `;
    }).join('');
  }

  function computeProgress(task) {
    // Simple heuristic based on state
    const weights = {
      GROUNDING: 5, PLANNING: 15, WAIT_APPROVAL: 25,
      EXECUTING: 45, EVALUATING: 60, REVIEWING: 75,
      FINAL_APPROVAL: 90, COMPLETED: 100, PAUSED: 0, ESCALATED: 0, ABORTED: 0,
    };
    const base = weights[task.state] || 0;
    // If we have task count info, refine
    if (task.total_tasks > 0 && task.completed_tasks !== undefined) {
      const taskPct = (task.completed_tasks / task.total_tasks) * 50; // execution accounts for 50%
      return Math.min(100, Math.round(base + taskPct));
    }
    return base;
  }

  // ─── Detail Page ──────────────────────────────────────
  async function showDetail(goalID) {
    document.getElementById('lht-list-page').style.display = 'none';
    document.getElementById('lht-detail-page').style.display = 'block';
    currentGoalID = goalID;

    try {
      const resp = await fetch('/api/v2/lht/' + goalID);
      if (!resp.ok) throw new Error('HTTP ' + resp.status);
      const detail = await resp.json();
      renderDetail(detail);
    } catch (err) {
      console.warn('[LHT] detail fetch failed:', err.message);
    }

    fetchLogs();
    fetchReview();
    subscribeSSE(goalID);
    updateControlButtons();
  }

  function renderDetail(detail) {
    if (!detail) return;

    document.getElementById('lht-detail-title').textContent = detail.description || detail.goal_id;

    const badge = STATE_BADGES[detail.state] || { cls: 'badge-default', icon: '❓', label: detail.state };
    document.getElementById('lht-detail-state').className = 'lht-badge ' + badge.cls;
    document.getElementById('lht-detail-state').textContent = badge.icon + ' ' + badge.label;

    document.getElementById('lht-detail-progress').textContent = computeProgress(detail) + '%';
    document.getElementById('lht-detail-budget').textContent =
      '预算: ' + (detail.used_rounds || 0) + '/' + (detail.max_rounds || '∞') + ' 轮';

    // Render steps
    if (detail.tasks && detail.tasks.length > 0) {
      const stepsHtml = detail.tasks.map((t, i) => {
        const status = t.state === 'COMPLETED' ? '✅' :
                       t.state === 'EXECUTING' ? '▶️' :
                       t.state === 'EVALUATING' ? '🔬' :
                       t.state === 'FAILED' ? '❌' : '⏳';
        return `<div class="lht-step-item">
          <span class="lht-step-status">${status}</span>
          <span class="lht-step-desc">${escHtml(t.description || 'Task ' + (i+1))}</span>
        </div>`;
      }).join('');
      document.getElementById('lht-steps-list').innerHTML = stepsHtml;
    }

    updateControlButtons();
  }

  async function fetchLogs() {
    if (!currentGoalID) return;
    try {
      const resp = await fetch('/api/v2/lht/' + currentGoalID + '/logs');
      if (!resp.ok) return;
      const logs = await resp.json();
      if (logs && logs.length > 0) {
        renderLogs(logs);
      }
    } catch (e) { /* API may not be ready */ }
  }

  function renderLogs(logs) {
    const container = document.getElementById('lht-stream-log');
    container.innerHTML = logs.map(l => {
      const time = l.timestamp ? new Date(l.timestamp).toLocaleTimeString() : '';
      return `<div class="lht-log-entry ${l.level || 'info'}">
        <span class="lht-log-time">${time}</span>
        <span class="lht-log-msg">${escHtml(l.message || l.content || '')}</span>
      </div>`;
    }).join('');
    container.scrollTop = container.scrollHeight;
  }

  function appendLog(entry) {
    const container = document.getElementById('lht-stream-log');
    // Remove loading placeholder
    if (container.querySelector('.lht-loading')) {
      container.innerHTML = '';
    }
    const time = entry.timestamp ? new Date(entry.timestamp).toLocaleTimeString() : new Date().toLocaleTimeString();
    const div = document.createElement('div');
    div.className = 'lht-log-entry ' + (entry.level || 'info');
    div.innerHTML = `<span class="lht-log-time">${time}</span><span class="lht-log-msg">${escHtml(entry.message || entry.content || '')}</span>`;
    container.appendChild(div);
    container.scrollTop = container.scrollHeight;
  }

  async function fetchReview() {
    if (!currentGoalID) return;
    try {
      const resp = await fetch('/api/v2/lht/' + currentGoalID + '/review');
      if (!resp.ok) return;
      const reviews = await resp.json();
      if (reviews && reviews.length > 0) {
        renderReviews(reviews);
      }
    } catch (e) { /* API may not be ready */ }
  }

  function renderReviews(reviews) {
    const container = document.getElementById('lht-review-content');
    container.innerHTML = reviews.map(r => `
      <div class="lht-review-card">
        <div class="lht-review-header">
          <span class="lht-badge ${r.verdict === 'PASS' ? 'badge-success' : 'badge-danger'}">
            ${r.verdict === 'PASS' ? '✅ PASS' : '❌ FAIL'}
          </span>
          <span>${escHtml(r.reviewer || '')} 轮次${r.round || ''}</span>
          ${r.score !== undefined ? '<span>评分: ' + r.score + '</span>' : ''}
        </div>
        ${r.issues && r.issues.length > 0 ? '<div class="lht-review-issues">' +
          r.issues.map(i => '<div class="lht-review-issue">• ' + escHtml(i) + '</div>').join('') +
        '</div>' : ''}
      </div>
    `).join('');
  }

  // ─── SSE ──────────────────────────────────────────────
  function subscribeSSE(goalID) {
    if (sseUnsub) sseUnsub();
    if (typeof Reef === 'undefined' || !Reef.EventBus) {
      // SSE not available yet, poll instead
      return;
    }
    sseUnsub = Reef.EventBus.on(/^lht_/, function(event) {
      if (event.goal_id !== goalID) return;
      switch (event.type) {
        case 'lht_state':
          updateState(event);
          break;
        case 'lht_log':
          appendLog(event);
          break;
        case 'lht_milestone':
          appendLog({ message: '🏁 里程碑: ' + event.message, level: 'milestone' });
          break;
      }
    });
  }

  function updateState(event) {
    if (!event || !event.state) return;
    const badge = STATE_BADGES[event.state] || { cls: 'badge-default', icon: '❓', label: event.state };
    const stateEl = document.getElementById('lht-detail-state');
    stateEl.className = 'lht-badge ' + badge.cls;
    stateEl.textContent = badge.icon + ' ' + badge.label;
    updateControlButtons();
    // Refresh detail view
    fetch('/api/v2/lht/' + currentGoalID)
      .then(r => r.json())
      .then(d => { if (d) renderDetail(d); })
      .catch(() => {});
  }

  // ─── Control Actions ──────────────────────────────────
  function updateControlButtons() {
    const detail = document.getElementById('lht-detail-page');
    if (detail.style.display === 'none') return;

    const stateEl = document.getElementById('lht-detail-state');
    const stateText = stateEl ? stateEl.textContent : '';

    const btnPause = document.getElementById('lht-btn-pause');
    const btnResume = document.getElementById('lht-btn-resume');
    const btnStop = document.getElementById('lht-btn-stop');
    const btnApprove = document.getElementById('lht-btn-approve');
    const btnReject = document.getElementById('lht-btn-reject');
    const replySection = document.getElementById('lht-reply-section');

    // Determine state from badge text
    let state = '';
    for (const [s, b] of Object.entries(STATE_BADGES)) {
      if (stateText.includes(b.icon)) { state = s; break; }
    }

    const isTerminal = (state === 'COMPLETED' || state === 'ABORTED');

    btnPause.style.display = (!isTerminal && state !== 'PAUSED') ? '' : 'none';
    btnResume.style.display = (state === 'PAUSED' || state === 'ESCALATED') ? '' : 'none';
    btnStop.style.display = !isTerminal ? '' : 'none';
    btnApprove.style.display = (state === 'WAIT_APPROVAL' || state === 'FINAL_APPROVAL') ? '' : 'none';
    btnReject.style.display = (state === 'WAIT_APPROVAL' || state === 'FINAL_APPROVAL') ? '' : 'none';

    // Show reply input for GROUNDING and ESCALATED states
    replySection.style.display = (state === 'GROUNDING' || state === 'ESCALATED') ? '' : 'none';
  }

  async function doAction(action) {
    if (!currentGoalID) return;
    try {
      const resp = await fetch('/api/v2/lht/' + currentGoalID + '/' + action, { method: 'POST' });
      if (resp.ok) {
        const result = await resp.json();
        if (result && result.state) {
          updateState({ state: result.state });
        }
      }
    } catch (err) {
      console.error('[LHT] action failed:', action, err);
    }
  }

  async function sendReply() {
    if (!currentGoalID) return;
    const input = document.getElementById('lht-reply-input');
    const content = input.value.trim();
    if (!content) return;

    try {
      const resp = await fetch('/api/v2/lht/' + currentGoalID + '/reply', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: content }),
      });
      if (resp.ok) {
        input.value = '';
        appendLog({ message: '💬 你: ' + content, level: 'user' });
      }
    } catch (err) {
      console.error('[LHT] reply failed:', err);
    }
  }

  async function insertRequirement() {
    if (!currentGoalID) return;
    const input = document.getElementById('lht-insert-input');
    const content = input.value.trim();
    if (!content) return;

    try {
      const resp = await fetch('/api/v2/lht/' + currentGoalID + '/insert', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ requirement: content }),
      });
      if (resp.ok) {
        input.value = '';
        appendLog({ message: '📌 新要求已插入: ' + content, level: 'insert' });
      }
    } catch (err) {
      console.error('[LHT] insert failed:', err);
    }
  }

  // ─── Dialog ───────────────────────────────────────────
  function showNewDialog() {
    document.getElementById('lht-new-dialog').style.display = 'flex';
    document.getElementById('lht-new-title').focus();
  }

  function hideNewDialog() {
    document.getElementById('lht-new-dialog').style.display = 'none';
  }

  async function createGoal() {
    const title = document.getElementById('lht-new-title').value.trim();
    if (!title) return;

    try {
      const resp = await fetch('/api/v2/lht/new', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: title }),
      });
      if (resp.ok) {
        hideNewDialog();
        fetchList();
      }
    } catch (err) {
      console.error('[LHT] create goal failed:', err);
    }
  }

  // ─── Navigation ───────────────────────────────────────
  function openDetail(goalID) {
    window.location.hash = '#/lht/' + goalID;
  }

  function backToList() {
    window.location.hash = '#/lht';
  }

  // ─── Utilities ────────────────────────────────────────
  function escHtml(s) {
    if (!s) return '';
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  // ─── Public API ───────────────────────────────────────
  return {
    show, hide, isActive,
    showList, fetchList,
    openDetail, backToList,
    showNewDialog, hideNewDialog, createGoal,
    doAction, sendReply, insertRequirement,
  };
})();
