document.addEventListener('alpine:init', () => {
  Alpine.data('deckApp', () => ({
    // ── State ──
    config: null,
    pages: [],
    activePage: '',
    view: 'grid',
    showAdvanced: false,
    showDisplay: false,
    toast: null,
    displayOutputs: {},
    isCapturingKey: false,

    // ── Edit state ──
    editing: null,
    editForm: {},

    // ── Sub-views ──
    subView: null,       // 'backups', 'settings', 'switcher', null

    // ── Settings ──
    settingsForm: {},
    autoSwitchRules: [],

    // ── Init ──
    async init() {
      await this.loadConfig()
      setInterval(() => this.pollDisplayOutputs(), 3000)
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
          this.activePage = this.config.default_page || this.pages[0].name
        }
        this.syncSettings()
      } catch (e) {
        this.showToast('Failed to load config', 'error')
      }
    },

    syncSettings() {
      this.settingsForm = {
        brightness: this.config.devices?.[0]?.brightness || 75,
        screensaver_enabled: this.config.screensaver?.enabled || false,
        screensaver_idle: this.config.screensaver?.idle_seconds || 30,
        screensaver_brightness: this.config.screensaver?.brightness || 10,
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

    // ── Grid ──
    get gridKeys() {
      const page = this.currentPage
      if (!page) return []
      const keys = []
      for (let i = 0; i < 15; i++) {
        const kc = page.keys.find(k => k.index === i)
        keys.push({
          index: i,
          ...kc,
          configured: !!kc,
          hasDisplay: !!(kc?.display),
          previewUrl: this.keyPreviewUrl(kc ? kc : {}),
        })
      }
      return keys
    },

    keyPreviewUrl(kc) {
      const k = kc || {}
      let url = '/api/render?key_size=72'
      if (k.icon) url += `&icon=${encodeURIComponent(k.icon)}`
      const label = this.displayOutputs[k.index] || k.label
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

    get keyChips() {
      if (!this.editForm.keys) return []
      const map = { ctrl: 'Ctrl', alt: 'Alt', shift: 'Shift', super: 'Super', meta: 'Super' }
      return this.editForm.keys.split('+').map(k => map[k] || k.charAt(0).toUpperCase() + k.slice(1))
    },

    toggleMod(mod) {
      if (mod === 'ctrl') this.editForm.modCtrl = !this.editForm.modCtrl
      else if (mod === 'alt') this.editForm.modAlt = !this.editForm.modAlt
      else if (mod === 'shift') this.editForm.modShift = !this.editForm.modShift
      else if (mod === 'super') this.editForm.modSuper = !this.editForm.modSuper
      this.rebuildKeys()
    },

    rebuildKeys() {
      const mods = []
      if (this.editForm.modCtrl) mods.push('ctrl')
      if (this.editForm.modAlt) mods.push('alt')
      if (this.editForm.modShift) mods.push('shift')
      if (this.editForm.modSuper) mods.push('super')
      if (this.editForm.mainKey) mods.push(this.editForm.mainKey)
      this.editForm.keys = mods.join('+')
    },

    startKeyCapture() {
      this.isCapturingKey = true
      this.$nextTick(() => this.$refs.keyCapture?.focus())
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
      this.editForm.mainKey = key.toLowerCase()
      this.isCapturingKey = false
      this.rebuildKeys()
    },

    displayKey(key) {
      const map = { return: '\u21B5', escape: 'Esc', tab: 'Tab', space: '\u2423',
        delete: 'Del', backspace: '\u232B', up: '\u2191', down: '\u2193', left: '\u2190', right: '\u2192' }
      return map[key] || key.toUpperCase()
    },

    // ── Edit Key ──
    editKey(idx) {
      this.isCapturingKey = false
      const page = this.currentPage
      if (!page) return
      const kc = page.keys.find(k => k.index === idx)
      this.editing = idx
      this.showAdvanced = false
      this.showDisplay = !!(kc?.display)

      const action = kc?.action || {}

      const parts = (action.keys || '').toLowerCase().split('+')
      const mainKey = parts.length > 0 ? parts.pop() : ''

      this.editForm = {
        icon: kc?.icon || '',
        label: kc?.label || '',
        icon_scale: kc?.icon_scale ?? 0.55,
        font_size: kc?.font_size ?? 16,
        bg_color: kc?.background || '',
        action_type: action.type || 'command',
        command: action.command || '',
        builtin: action.builtin || '',
        script: action.script || '',
        page: action.page || '',
        keys: action.keys || '',
        modCtrl: parts.includes('ctrl'),
        modAlt: parts.includes('alt'),
        modShift: parts.includes('shift'),
        modSuper: parts.includes('super'),
        mainKey: mainKey,
        background: action.background ?? true,
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
      const actionType = this.editForm.action_type

      if (dm === 'command' && !this.editForm.display_command) {
        this.showToast('Display command is required', 'error')
        return
      }
      if (dm === 'script' && !this.editForm.display_script) {
        this.showToast('Display script path is required', 'error')
        return
      }

      if (actionType && actionType !== 'none') {
        if (actionType === 'command' && !this.editForm.command) {
          if (!hasDisplay) { this.showToast('Command is required', 'error'); return }
        }
        if (actionType === 'script' && !this.editForm.script) {
          if (!hasDisplay) { this.showToast('Script path is required', 'error'); return }
        }
        if (actionType === 'page' && !this.editForm.page) {
          this.showToast('Target page is required', 'error')
          return
        }
        if (actionType === 'keyboard' && !this.editForm.keys) {
          if (!hasDisplay) { this.showToast('Key combination is required', 'error'); return }
        }
      }

      const kc = {
        index: this.editing,
        icon: this.editForm.icon,
        label: this.editForm.label,
        icon_scale: this.editForm.icon_scale,
        font_size: this.editForm.font_size,
        background: this.editForm.bg_color || '',
      }

      if (actionType && actionType !== 'none') {
        if (actionType === 'command' && this.editForm.command) {
          kc.action = { type: 'command', command: this.editForm.command, background: this.editForm.background }
        } else if (actionType === 'builtin') {
          kc.action = { type: 'builtin', builtin: this.editForm.builtin }
        } else if (actionType === 'script' && this.editForm.script) {
          kc.action = { type: 'script', script: this.editForm.script }
        } else if (actionType === 'page' && this.editForm.page) {
          kc.action = { type: 'page', page: this.editForm.page }
        } else if (actionType === 'keyboard' && this.editForm.keys) {
          kc.action = { type: 'keyboard', keys: this.editForm.keys }
        } else if (!hasDisplay) {
          kc.action = null
        } else {
          kc.action = null
        }
      } else {
        kc.action = null
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
      } else {
        kc.display = null
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
      this.displayOutputs = {}
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

      // Update page references
      for (const p of this.pages) {
        for (const k of p.keys) {
          if (k.action?.page === oldName) k.action.page = newName.trim()
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
      this.config.screensaver = {
        enabled: this.settingsForm.screensaver_enabled,
        idle_seconds: parseInt(this.settingsForm.screensaver_idle) || 30,
        brightness: parseInt(this.settingsForm.screensaver_brightness) || 10,
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
      // Extract date from config.YYYY-MM-DDTHH-MM-SS.json
      const m = name.match(/config\.(.+)\.json/)
      if (!m) return name
      return m[1].replace('T', ' ')
    },
  }))
})
