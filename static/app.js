document.addEventListener('alpine:init', () => {
  Alpine.data('deckApp', () => ({
    // ── State ──
    config: null,
    pages: [],
    activePage: '',
    view: 'grid',
    showAdvanced: false,
    toast: null,
    displayOutputs: {},
    isCapturingKey: false,
    decks: [],
    activeDeckSerial: '',

    // ── Edit state ──
    editing: null,
    editForm: {
      actions: {
        tap: {},
        long_press: {},
        double_tap: {},
        hold: { start: {}, end: {} },
      },
    },

    // ── Sub-views ──
    subView: null,

    // ── Settings ──
    settingsForm: {},
    autoSwitchRules: [],

    // ── Constants ──
    actionIconMap: {
      command: 'terminal',
      builtin: 'cog',
      script: 'file-code',
      page: 'layer-group',
      keyboard: 'keyboard',
    },
    actionTitleMap: {
      command: 'Command',
      builtin: 'Built-in',
      script: 'Script',
      page: 'Switch page',
      keyboard: 'Keyboard shortcut',
    },

    // ── Init ──
    async init() {
      await this.loadConfig()
      await this.loadDecks()
      setInterval(() => this.pollDisplayOutputs(), 3000)
      this.connectSSE()
    },

    connectSSE() {
      if (this._sse) this._sse.close()
      const es = new EventSource('/api/events')
      this._sse = es
      es.addEventListener('page_changed', (e) => {
        try {
          const data = JSON.parse(e.data)
          if (data.page && this.pages.find(p => p.name === data.page)) {
            this.activePage = data.page
            localStorage.setItem('sd_active_page', data.page)
            this.displayOutputs = {}
          }
        } catch (_) {}
      })
      es.onerror = () => {
        es.close()
        this._sse = null
        setTimeout(() => this.connectSSE(), 3000)
      }
    },

    async loadDecks() {
      try {
        const res = await fetch('/api/decks')
        if (res.ok) {
          this.decks = await res.json()
          if (this.decks.length > 0 && !this.activeDeckSerial) {
            this.activeDeckSerial = this.decks[0].serial
          }
        }
      } catch (_) {}
    },

    async pollDisplayOutputs() {
      if (this.view !== 'grid') return
      try {
        const res = await fetch(`/api/display-outputs`)
        this.displayOutputs = await res.json()
      } catch (_) {}
    },

    async loadConfig() {
      try {
        const res = await fetch('/api/config')
        this.config = await res.json()
        this.pages = this.config.pages
        if (this.pages.length > 0) {
          const saved = localStorage.getItem('sd_active_page')
          if (saved && this.pages.find(p => p.name === saved)) {
            this.activePage = saved
          } else {
            this.activePage = this.config.default_page || this.pages[0].name
          }
        }
        this.syncSettings()
      } catch (e) {
        this.showToast('Failed to load config', 'error')
      }
    },

    syncSettings() {
      this.settingsForm = {
        brightness: this.config.devices?.[0]?.brightness || 75,
        font: this.config.font || 'medium',
        screensaver_enabled: this.config.screensaver?.enabled || false,
        screensaver_idle: this.config.screensaver?.idle_seconds || 30,
        screensaver_brightness: this.config.screensaver?.brightness || 10,
        long_press_ms: this.config.timing?.long_press_ms || 500,
        double_tap_ms: this.config.timing?.double_tap_ms || 300,
      }
      this.autoSwitchRules = [...(this.config.auto_switch || [])]
    },

    // ── Page helpers ──
    get currentPage() {
      return this.pages.find(p => p.name === this.activePage)
    },

    get pageNames() {
      return this.pages.map(p => p.name)
    },

    get activeDeck() {
      return this.decks.find(d => d.serial === this.activeDeckSerial)
        || { keys_x: 5, keys_y: 3, num_keys: 15 }
    },

    // ── Grid ──
    get gridKeys() {
      const page = this.currentPage
      if (!page) return []
      const keys = []
      const n = this.activeDeck.num_keys
      for (let i = 0; i < n; i++) {
        const kc = page.keys.find(k => k.index === i)
        const dout = this.displayOutputs[i]
        keys.push({
          index: i,
          ...kc,
          configured: !!kc,
          hasDisplay: !!(kc?.display),
          hasAction: !!(kc?.actions?.length),
          actionTypes: this.getActionTypes(kc?.actions || []),
          displayBg: dout?.background || '',
          displayText: dout?.text || '',
          previewUrl: this.keyPreviewUrl(kc ? kc : {}, dout),
        })
      }
      return keys
    },

    getActionTypes(actions) {
      const seen = {}
      for (const a of actions) {
        if (a.type && a.type !== 'none') {
          seen[a.type] = true
        }
      }
      return Object.keys(seen)
    },

    keyPreviewUrl(kc, dout) {
      const k = kc || {}
      let url = '/api/render?key_size=72'
      if (k.icon) url += `&icon=${encodeURIComponent(k.icon)}`
      const label = dout?.text || this.displayOutputs[k.index]?.text || k.label
      if (label) url += `&label=${encodeURIComponent(label)}`
      if (k.icon_scale != null) url += `&icon_scale=${k.icon_scale}`
      if (k.font_size != null) url += `&font_size=${k.font_size}`
      if (k.background) url += `&background=${encodeURIComponent(k.background)}`
      return url
    },

    isFAIcon(icon) {
      if (!icon) return false
      return icon.startsWith('fa:') || icon.startsWith('far:') || icon.startsWith('fab:')
    },

    faClass(icon) {
      if (!icon) return ''
      if (icon.startsWith('fa:')) return 'fa-solid fa-' + icon.slice(3)
      if (icon.startsWith('far:')) return 'fa-regular fa-' + icon.slice(4)
      if (icon.startsWith('fab:')) return 'fa-brands fa-' + icon.slice(4)
      return ''
    },

    // ── Multi-action helpers ──
    getCurrentAction() {
      const t = this.editForm.activeTrigger || 'tap'
      if (t === 'hold') {
        return this.editForm.actions?.hold?.[this.editForm.holdPhase || 'start']
      }
      return this.editForm.actions?.[t]
    },

    hasCurrentAction(trigger) {
      const a = this.editForm.actions?.[trigger]
      return a && a.type && a.type !== 'none'
    },

    hasHoldAction(phase) {
      const a = this.editForm.actions?.hold?.[phase]
      return a && a.type && a.type !== 'none'
    },

    triggerLabel(trigger) {
      return { tap: 'Tap', long_press: 'Long Press', double_tap: 'Double Tap', hold: 'Hold' }[trigger] || trigger
    },

    // ── Keyboard helpers (for current action) ──
    get keyChips() {
      const a = this.getCurrentAction()
      if (!a || !a.keys) return []
      const map = { ctrl: 'Ctrl', alt: 'Alt', shift: 'Shift', super: 'Super', meta: 'Super' }
      return a.keys.split('+').map(k => map[k] || k.charAt(0).toUpperCase() + k.slice(1))
    },

    toggleMod(mod) {
      const a = this.getCurrentAction()
      if (!a) return
      if (mod === 'ctrl') a.modCtrl = !a.modCtrl
      else if (mod === 'alt') a.modAlt = !a.modAlt
      else if (mod === 'shift') a.modShift = !a.modShift
      else if (mod === 'super') a.modSuper = !a.modSuper
      this.rebuildKeys()
    },

    rebuildKeys() {
      const a = this.getCurrentAction()
      if (!a) return
      const mods = []
      if (a.modCtrl) mods.push('ctrl')
      if (a.modAlt) mods.push('alt')
      if (a.modShift) mods.push('shift')
      if (a.modSuper) mods.push('super')
      if (a.mainKey) mods.push(a.mainKey)
      a.keys = mods.join('+')
    },

    startKeyCapture() {
      this.isCapturingKey = true
      this.$nextTick(() => {
        this.$el.querySelectorAll('.key-capture').forEach(el => {
          if (el.offsetParent !== null) el.focus()
        })
      })
    },

    onCaptureKey(e) {
      if (!this.isCapturingKey) return
      e.preventDefault()
      e.stopPropagation()
      const map = {
        'Control': '', 'Alt': '', 'Shift': '', 'Meta': '',
        ' ': 'space', 'Enter': 'return', 'Tab': 'tab',
        'Escape': 'escape', 'Delete': 'delete', 'Backspace': 'backspace',
        'ArrowUp': 'up', 'ArrowDown': 'down', 'ArrowLeft': 'left', 'ArrowRight': 'right',
      }
      let key = map[e.key]
      if (key === undefined) {
        key = e.key.length === 1 ? e.key.toLowerCase() : e.key
      }
      if (!key) return
      const a = this.getCurrentAction()
      if (a) a.mainKey = key.toLowerCase()
      this.isCapturingKey = false
      this.rebuildKeys()
    },

    displayKey(key) {
      const map = { return: '\u21B5', escape: 'Esc', tab: 'Tab', space: '\u2423',
        delete: 'Del', backspace: '\u232B', up: '\u2191', down: '\u2193', left: '\u2190', right: '\u2192' }
      return map[key] || key.toUpperCase()
    },

    // ── Edit Key ──
    defaultAction(trigger) {
      return { trigger, type: 'none', command: '', builtin: '', script: '', page: '', keys: '', background: true, modCtrl: false, modAlt: false, modShift: false, modSuper: false, mainKey: '' }
    },

    editKey(idx) {
      this.isCapturingKey = false
      const page = this.currentPage
      if (!page) return
      const kc = page.keys.find(k => k.index === idx)
      this.editing = idx
      this.showAdvanced = false

      const existing = kc?.actions || []

      const tap        = existing.find(a => a.trigger === 'tap') || null
      const longPress  = existing.find(a => a.trigger === 'long_press') || null
      const doubleTap  = existing.find(a => a.trigger === 'double_tap') || null
      const holdStart  = existing.find(a => a.trigger === 'hold_start') || null
      const holdEnd    = existing.find(a => a.trigger === 'hold_end') || null

      const build = (src, trigger) => {
        const def = this.defaultAction(trigger)
        if (!src) return def
        const parts = (src.keys || '').toLowerCase().split('+')
        const main = parts.length > 0 ? parts.pop() : ''
        return {
          ...def,
          type: src.type || 'none',
          command: src.command || '',
          builtin: src.builtin || '',
          script: src.script || '',
          page: src.page || '',
          keys: src.keys || '',
          background: src.background ?? true,
          modCtrl: parts.includes('ctrl'),
          modAlt: parts.includes('alt'),
          modShift: parts.includes('shift'),
          modSuper: parts.includes('super'),
          mainKey: main,
        }
      }

      this.editForm = {
        icon: kc?.icon || '',
        label: kc?.label || '',
        icon_scale: kc?.icon_scale ?? 0.55,
        font_size: kc?.font_size ?? 16,
        bg_color: kc?.background || '',
        activeTrigger: 'tap',
        holdPhase: 'start',
        actions: {
          tap: build(tap, 'tap'),
          long_press: build(longPress, 'long_press'),
          double_tap: build(doubleTap, 'double_tap'),
          hold: {
            start: build(holdStart, 'hold_start'),
            end: build(holdEnd, 'hold_end'),
          },
        },
        display_mode: kc?.display ? (kc.display.command ? 'command' : 'script') : 'none',
        display_command: kc?.display?.command || '',
        display_script: kc?.display?.script || '',
        display_interval: kc?.display?.interval || '30s',
        display_max_len: kc?.display?.max_len || 128,
        display_timeout: kc?.display?.timeout || '',
      }
    },

    get editPreviewUrl() {
      const f = this.editForm
      let url = '/api/render?key_size=96'
      if (f.icon) url += `&icon=${encodeURIComponent(f.icon)}`
      if (f.label) url += `&label=${encodeURIComponent(f.label)}`
      if (f.icon_scale != null) url += `&icon_scale=${f.icon_scale}`
      if (f.font_size != null) url += `&font_size=${f.font_size}`
      if (f.bg_color) url += `&background=${encodeURIComponent(f.bg_color)}`
      return url
    },

    get editPreviewFaClass() {
      return this.faClass(this.editForm.icon)
    },

    async saveKey() {
      const page = this.currentPage
      if (!page) return

      const dm = this.editForm.display_mode
      const hasDisplay = dm !== 'none'

      if (dm === 'command' && !this.editForm.display_command) {
        this.showToast('Display command is required', 'error')
        return
      }
      if (dm === 'script' && !this.editForm.display_script) {
        this.showToast('Display script path is required', 'error')
        return
      }

      // Validate and collect actions
      const actions = []
      const checkAction = (a) => {
        if (!a || !a.type || a.type === 'none') return
        if (a.type === 'command' && !a.command) return
        if (a.type === 'script' && !a.script) return
        if (a.type === 'page' && !a.page) return
        if (a.type === 'keyboard' && !a.keys) return
        const entry = {
          trigger: a.trigger,
          type: a.type,
          command: a.type === 'command' ? a.command : undefined,
          builtin: a.type === 'builtin' ? a.builtin : undefined,
          script: a.type === 'script' ? a.script : undefined,
          page: a.type === 'page' ? a.page : undefined,
          keys: a.type === 'keyboard' ? a.keys : undefined,
          background: a.background || undefined,
        }
        // Clean undefined
        for (const k of Object.keys(entry)) {
          if (entry[k] === undefined) delete entry[k]
        }
        actions.push(entry)
      }
      checkAction(this.editForm.actions.tap)
      checkAction(this.editForm.actions.long_press)
      checkAction(this.editForm.actions.double_tap)
      checkAction(this.editForm.actions.hold.start)
      checkAction(this.editForm.actions.hold.end)

      const kc = {
        index: this.editing,
        icon: this.editForm.icon || undefined,
        label: this.editForm.label || undefined,
        icon_scale: this.editForm.icon_scale !== 0.55 ? this.editForm.icon_scale : undefined,
        font_size: this.editForm.font_size !== 16 ? this.editForm.font_size : undefined,
        background: this.editForm.bg_color || undefined,
      }

      if (actions.length > 0) {
        kc.actions = actions
      }

      if (hasDisplay) {
        kc.display = {
          command: dm === 'command' ? this.editForm.display_command : '',
          script: dm === 'script' ? this.editForm.display_script : '',
          interval: this.editForm.display_interval || '30s',
          max_len: this.editForm.display_max_len || 0,
        }
        if (this.editForm.display_timeout) {
          kc.display.timeout = this.editForm.display_timeout
        }
      }

      const existingIdx = page.keys.findIndex(k => k.index === this.editing)
      if (existingIdx >= 0) {
        page.keys[existingIdx] = kc
      } else {
        page.keys.push(kc)
      }

      await this.saveConfig()
      this.editing = null
    },

    async deleteKey() {
      const page = this.currentPage
      if (!page) return
      page.keys = page.keys.filter(k => k.index !== this.editing)
      await this.saveConfig()
      this.editing = null
    },

    // ── Image picker ──
    pickImage() {
      this.$refs.fileInput.click()
    },

    async onImagePicked(e) {
      const file = e.target.files[0]
      if (!file) return
      const formData = new FormData()
      formData.append('file', file)
      try {
        const res = await fetch('/api/upload', { method: 'POST', body: formData })
        if (!res.ok) { this.showToast('Upload failed', 'error'); return }
        const data = await res.json()
        this.editForm.icon = data.path
      } catch (err) {
        this.showToast('Upload failed', 'error')
      }
      e.target.value = ''
    },

    // ── Page management ──
    switchPage(name) {
      this.activePage = name
      localStorage.setItem('sd_active_page', name)
      this.displayOutputs = {}
      fetch('/api/activate-page', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ page: name })
      }).catch(() => {})
    },

    async addPage() {
      const name = prompt('New page name:')
      if (!name || name.trim() === '') return
      if (this.pages.find(p => p.name === name)) {
        this.showToast('Page already exists', 'error')
        return
      }
      this.pages.push({ name: name.trim(), keys: [] })
      this.activePage = name.trim()
      this.config.pages = this.pages
      if (!this.config.default_page) {
        this.config.default_page = name.trim()
      }
      await this.saveConfig()
    },

    async deletePage() {
      if (this.pages.length <= 1) {
        this.showToast('Cannot delete the last page', 'error')
        return
      }
      if (!confirm(`Delete page "${this.activePage}"?`)) return
      const oldName = this.activePage
      this.pages = this.pages.filter(p => p.name !== oldName)
      this.activePage = this.pages[0].name
      if (this.config.default_page === oldName) {
        this.config.default_page = this.activePage
      }
      this.config.pages = this.pages
      await this.saveConfig()
    },

    async renamePage() {
      const oldName = this.activePage
      const newName = prompt('Rename page:', oldName)
      if (!newName || newName.trim() === '' || newName === oldName) return
      if (this.pages.find(p => p.name === newName.trim())) {
        this.showToast('Page already exists', 'error')
        return
      }
      const page = this.pages.find(p => p.name === oldName)
      if (page) page.name = newName.trim()
      this.activePage = newName.trim()
      if (this.config.default_page === oldName) {
        this.config.default_page = newName.trim()
      }

      for (const p of this.pages) {
        for (const k of p.keys) {
          for (const a of (k.actions || [])) {
            if (a.page === oldName) a.page = newName.trim()
          }
        }
      }
      for (const r of (this.config.auto_switch || [])) {
        if (r.page === oldName) r.page = newName.trim()
      }

      await this.saveConfig()
    },

    // ── Save / Reload ──
    async saveConfig() {
      this.config.pages = this.pages
      try {
        const res = await fetch('/api/config', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(this.config),
        })
        if (res.ok) {
          this.showToast('Saved', 'success')
        } else {
          const text = await res.text()
          this.showToast(`Save failed: ${text}`, 'error')
        }
      } catch (e) {
        this.showToast('Save failed', 'error')
      }
    },

    async reloadConfig() {
      await this.loadConfig()
      this.showToast('Reloaded', 'success')
    },

    // ── Settings ──
    async saveSettings() {
      if (!this.config.devices || this.config.devices.length === 0) {
        this.config.devices = [{ serial: '', brightness: 75 }]
      }
      this.config.devices[0].brightness = parseInt(this.settingsForm.brightness)
      this.config.font = this.settingsForm.font || 'medium'
      this.config.screensaver = {
        enabled: this.settingsForm.screensaver_enabled,
        idle_seconds: parseInt(this.settingsForm.screensaver_idle) || 30,
        brightness: parseInt(this.settingsForm.screensaver_brightness) || 10,
      }
      this.config.timing = {
        long_press_ms: parseInt(this.settingsForm.long_press_ms) || 500,
        double_tap_ms: parseInt(this.settingsForm.double_tap_ms) || 300,
      }
      this.config.auto_switch = this.autoSwitchRules
      await this.saveConfig()
    },

    addAutoSwitchRule() {
      this.autoSwitchRules.push({ wm_class: '', title: '', page: '', stay: false, devices: [] })
    },

    removeAutoSwitchRule(idx) {
      this.autoSwitchRules.splice(idx, 1)
    },

    // ── Backup ──
    backups: [],

    async loadBackups() {
      try {
        const res = await fetch('/api/backups')
        this.backups = await res.json()
      } catch (e) {
        this.backups = []
      }
    },

    downloadConfig() {
      window.open('/api/config/download', '_blank')
    },

    async downloadBackup(name) {
      window.open(`/api/backups/${encodeURIComponent(name)}`, '_blank')
    },

    async restoreConfig() {
      const input = document.createElement('input')
      input.type = 'file'
      input.accept = '.json'
      input.onchange = async () => {
        const file = input.files[0]
        if (!file) return
        try {
          const formData = new FormData()
          formData.append('file', file)
          const res = await fetch('/api/config/restore', {
            method: 'POST',
            body: formData,
          })
          if (res.ok) {
            await this.loadConfig()
            this.showToast('Config restored', 'success')
          } else {
            const text = await res.text()
            this.showToast(`Restore failed: ${text}`, 'error')
          }
        } catch (e) {
          this.showToast('Restore failed', 'error')
        }
      }
      input.click()
    },

    async restoreBackup(filename) {
      if (!confirm(`Restore "${filename}"?`)) return
      try {
        const res = await fetch(`/api/backups/${encodeURIComponent(filename)}`)
        if (!res.ok) {
          this.showToast('Failed to download backup', 'error')
          return
        }
        const blob = await res.blob()
        const formData = new FormData()
        formData.append('file', blob, filename)
        const putRes = await fetch('/api/config/restore', {
          method: 'POST',
          body: formData,
        })
        if (putRes.ok) {
          await this.loadConfig()
          this.showToast('Backup restored', 'success')
        } else {
          const text = await putRes.text()
          this.showToast(`Restore failed: ${text}`, 'error')
        }
      } catch (e) {
        this.showToast('Restore failed', 'error')
      }
    },

    // ── Toast ──
    showToast(msg, type = 'success') {
      this.toast = { msg, type }
      setTimeout(() => { this.toast = null }, 2500)
    },

    // ── Formatting ──
    formatSize(bytes) {
      if (bytes < 1024) return bytes + ' B'
      return (bytes / 1024).toFixed(1) + ' KB'
    },

    formatTime(iso) {
      if (!iso) return ''
      const d = new Date(iso)
      return d.toLocaleString()
    },

    backupNameTime(name) {
      const m = name.match(/config\.(.+)\.json/)
      if (!m) return name
      return m[1].replace('T', ' ')
    },
  }))
})
