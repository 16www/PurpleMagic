// Adapted from Cec1c/cpamp-theme-studio 4e85a44 (MIT). See ../THIRD_PARTY_NOTICES.md.
(function () {
  'use strict';

  var script = document.currentScript;
  var scriptURL;
  try {
    scriptURL = new URL(script && script.src ? script.src : window.location.href);
  } catch (_) {
    scriptURL = { searchParams: { get: function () { return ''; } } };
  }

  var openOnLoad = scriptURL.searchParams.get('open') === '1';
  var runtimeVersion = scriptURL.searchParams.get('v') || 'dev';
  var hostWindow = resolveHostWindow();

  var existingRuntime = findRuntime(hostWindow);
  if (existingRuntime) {
    if (typeof existingRuntime.ensure === 'function') existingRuntime.ensure();
    if (openOnLoad && typeof existingRuntime.open === 'function') existingRuntime.open();
    return;
  }

  install(hostWindow, openOnLoad);

  function resolveHostWindow() { return window; }

  function findRuntime(win) {
    try {
      var doc = win.document;
      var mount = doc.getElementById('cpamp-theme-studio-root');
      return win.__SOFT_THEME_STUDIO__ ||
        (doc.documentElement && doc.documentElement.__SOFT_THEME_STUDIO__) ||
        (mount && mount.__SOFT_THEME_STUDIO__) || null;
    } catch (_) {
      return null;
    }
  }

  function resourceAssetURL(asset) {
    var name = asset === 'font-regular' ? 'JetBrainsMono-Regular.woff2' : 'JetBrainsMono-SemiBold.woff2';
    return new URL('fonts/' + name, scriptURL.href).href;
  }

  function install(win, shouldOpen) {
    var doc = win.document;
    var root = doc.documentElement;
    var storage;
    try { storage = win.localStorage; } catch (_) { storage = null; }
    var storageKey = 'soft-gm-console.theme';
    var legacyStorageKey = 'soft-gm-console.theme-v1';
    var themeStoreKey = 'soft-gm-console.host-theme';
    var effectsStoreKey = 'soft-gm-console.visual-effects';
    var styleID = 'cpamp-theme-studio-theme';
    var mountID = 'cpamp-theme-studio-root';
    var mount = null;
    var shadow = null;
    var observer = null;
    var hostThemeControl = null;
    var remountScheduled = false;
    var openRequested = Boolean(shouldOpen);
    var previousOverflow = '';
    var overflowOwner = null;
    var returnFocus = null;
    var fontRegularURL = resourceAssetURL('font-regular');
    var fontSemiBoldURL = resourceAssetURL('font-semibold');

    var defaults = {
      mode: 'auto',
      preset: 'coral-mist',
      radius: 'default',
      font: 'system',
      density: 'default',
      contentLayout: 'full',
      customAccent: '#3b82f6',
      effects: 'full'
    };

    // These palettes were designed specifically for Theme Studio. They do not
    // copy the named presets or palette data from new-api.
    var palettes = {
      cpamp: null,
      'ember-dusk': ['#bd5738', '#e09a55', '#fff7f1', '#1c120e'],
      'jade-circuit': ['#147d69', '#45a78e', '#f1faf7', '#081713'],
      'coral-mist': ['#cc4f69', '#df929f', '#fff5f7', '#1c1015'],
      'glacier-signal': ['#2878c7', '#43a3b2', '#f2f8fd', '#081521'],
      'solar-flare': ['#ce6728', '#dca83d', '#fff8eb', '#1b1409'],
      'moss-stone': ['#4d7652', '#84905b', '#f4f8f0', '#10170e'],
      'deep-current': ['#345fca', '#5d4bc1', '#f3f5ff', '#0b1020'],
      'amethyst-fog': ['#8153c2', '#ae6998', '#faf5ff', '#17101d'],
      graphite: ['#363b43', '#747b85', '#f5f6f7', '#111317']
    };

    var presetOrder = [
      'cpamp', 'ember-dusk', 'jade-circuit', 'coral-mist', 'glacier-signal',
      'solar-flare', 'moss-stone', 'deep-current', 'amethyst-fog', 'graphite'
    ];

    var legacyPresetMap = {
      default: 'cpamp', anthropic: 'ember-dusk', underground: 'jade-circuit',
      'rose-garden': 'coral-mist', 'lake-view': 'glacier-signal',
      'sunset-glow': 'solar-flare', 'forest-whisper': 'moss-stone',
      'ocean-breeze': 'deep-current', 'lavender-dream': 'amethyst-fog',
      monochrome: 'graphite', custom: 'custom'
    };

    var copy = {
      'zh-CN': {
        open: '打开主题工作室', title: '主题工作室', subtitle: '调整配色与布局，即时预览', close: '关闭', reset: '恢复默认',
        mode: '显示模式', auto: '跟随系统', light: '浅色', dark: '深色', palette: '配色', custom: '自定义强调色',
        radius: '圆角', square: '直角', small: '小', medium: '中', large: '大', xlarge: '超大', normal: '默认',
        font: '字体', hostFont: '工作台默认', jetBrainsMono: 'JetBrains Mono', systemSans: '系统无衬线',
        density: '界面密度', compact: '紧凑', comfortable: '舒适',
        layout: '内容宽度', full: '铺满', centered: '居中', effects: '视觉效果', rich: '完整效果', efficient: '性能优先',
        scopeHost: 'PurpleMagic · 外观偏好', scopeFrame: '外观偏好保存在当前浏览器',
        pCpamp: '晴空蓝', pEmber: '余烬暮色', pJade: '翡翠电路', pCoral: '珊瑚薄雾', pGlacier: '冰川信号',
        pSolar: '日冕', pMoss: '苔石', pDeep: '深流', pAmethyst: '紫晶雾', pGraphite: '石墨'
      },
      'zh-TW': {
        open: '開啟主題工作室', title: '主題工作室', subtitle: '調整 PurpleMagic 的配色與版面', close: '關閉', reset: '恢復預設',
        mode: '顯示模式', auto: '跟隨系統', light: '淺色', dark: '深色', palette: '配色', custom: '自訂強調色',
        radius: '圓角', square: '直角', small: '小', medium: '中', large: '大', xlarge: '超大', normal: '預設',
        font: '字體', hostFont: '預設字體', jetBrainsMono: 'JetBrains Mono', systemSans: '系統無襯線',
        density: '介面密度', compact: '緊湊', comfortable: '舒適',
        layout: '內容寬度', full: '鋪滿', centered: '置中', effects: '視覺效果', rich: '完整效果', efficient: '效能優先',
        scopeHost: '已連接宿主面板', scopeFrame: '外觀偏好儲存在目前瀏覽器',
        pCpamp: '晴空藍', pEmber: '餘燼暮色', pJade: '翡翠電路', pCoral: '珊瑚薄霧', pGlacier: '冰川訊號',
        pSolar: '日冕', pMoss: '苔石', pDeep: '深流', pAmethyst: '紫晶霧', pGraphite: '石墨'
      },
      en: {
        open: 'Open Theme Studio', title: 'Theme Studio', subtitle: 'Customize PurpleMagic colors and layout', close: 'Close', reset: 'Restore defaults',
        mode: 'Display mode', auto: 'System', light: 'Light', dark: 'Dark', palette: 'Palette', custom: 'Custom accent',
        radius: 'Corner radius', square: 'Square', small: 'Small', medium: 'Medium', large: 'Large', xlarge: 'Extra large', normal: 'Default',
        font: 'Typography', hostFont: 'Default font', jetBrainsMono: 'JetBrains Mono', systemSans: 'System sans',
        density: 'Density', compact: 'Compact', comfortable: 'Comfortable',
        layout: 'Content width', full: 'Full width', centered: 'Centered', effects: 'Visual effects', rich: 'Full effects', efficient: 'Performance',
        scopeHost: 'Connected to the host panel', scopeFrame: 'Appearance preferences are saved in this browser',
        pCpamp: 'Sky Blue', pEmber: 'Ember Dusk', pJade: 'Jade Circuit', pCoral: 'Coral Mist', pGlacier: 'Glacier Signal',
        pSolar: 'Solar Flare', pMoss: 'Moss Stone', pDeep: 'Deep Current', pAmethyst: 'Amethyst Fog', pGraphite: 'Graphite'
      },
      ru: {
        open: 'Открыть студию тем', title: 'Студия тем', subtitle: 'Настройте оформление PurpleMagic', close: 'Закрыть', reset: 'Сбросить',
        mode: 'Режим', auto: 'Системный', light: 'Светлый', dark: 'Тёмный', palette: 'Палитра', custom: 'Свой акцент',
        radius: 'Скругление', square: 'Без скругления', small: 'Малое', medium: 'Среднее', large: 'Большое', xlarge: 'Очень большое', normal: 'По умолчанию',
        font: 'Шрифт', hostFont: 'Шрифт по умолчанию', jetBrainsMono: 'JetBrains Mono', systemSans: 'Системный шрифт',
        density: 'Плотность', compact: 'Компактная', comfortable: 'Просторная',
        layout: 'Ширина', full: 'На всю ширину', centered: 'По центру', effects: 'Эффекты', rich: 'Полные', efficient: 'Производительность',
        scopeHost: 'Подключено к панели', scopeFrame: 'Настройки оформления сохраняются в этом браузере',
        pCpamp: 'Sky Blue', pEmber: 'Ember Dusk', pJade: 'Jade Circuit', pCoral: 'Coral Mist', pGlacier: 'Glacier Signal',
        pSolar: 'Solar Flare', pMoss: 'Moss Stone', pDeep: 'Deep Current', pAmethyst: 'Amethyst Fog', pGraphite: 'Graphite'
      }
    };

    var language = detectLanguage();
    var state = readState();
    var hostConnected = hostWindow !== window || Boolean(script && script.hasAttribute && script.hasAttribute('data-cpamp-theme-studio-loader'));
    var hostThemeLabels = {
      theme: true,
      '主题': true,
      '主題': true,
      'тема': true,
      'open theme studio': true,
      '打开主题工作室': true,
      '開啟主題工作室': true,
      'открыть студию темы': true,
      'открыть студию тем': true
    };

    function tr(key) {
      return (copy[language] && copy[language][key]) || copy.en[key] || key;
    }

    function detectLanguage() {
      var saved = readZustand('soft-gm-console.language');
      var candidate = saved && saved.state && saved.state.language;
      if (copy[candidate]) return candidate;
      var value = String(root.lang || win.navigator.language || 'en').toLowerCase();
      if (value.indexOf('zh-tw') === 0 || value.indexOf('zh-hk') === 0) return 'zh-TW';
      if (value.indexOf('zh') === 0) return 'zh-CN';
      if (value.indexOf('ru') === 0) return 'ru';
      return 'en';
    }

    function safeParse(value) {
      if (!value || typeof value !== 'string' || value.length > 32768) return null;
      try {
        var parsed = JSON.parse(value);
        return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : null;
      } catch (_) {
        return null;
      }
    }

    function storageGet(key) {
      try { return storage.getItem(key); } catch (_) { return null; }
    }

    function storageSet(key, value) {
      try { storage.setItem(key, value); } catch (_) { /* unavailable storage */ }
    }

    function readZustand(key) {
      return safeParse(storageGet(key));
    }

    function allowed(value, choices, fallback) {
      return choices.indexOf(value) >= 0 ? value : fallback;
    }

    function normalizeHex(value, fallback) {
      var normalized = typeof value === 'string' ? value.trim().toLowerCase() : '';
      return /^#[0-9a-f]{6}$/.test(normalized) ? normalized : fallback;
    }

    function readState() {
      var stored = safeParse(storageGet(storageKey));
      var source = stored && stored.schema === 2 && stored.state ? stored.state : null;
      if (!source) {
        var legacy = safeParse(storageGet(legacyStorageKey));
        source = legacy && legacy.state ? legacy.state : legacy;
        if (source && legacyPresetMap[source.preset]) {
          source = cloneState(source);
          source.preset = legacyPresetMap[source.preset];
        }
      }
      source = source || {};
      var hostTheme = readZustand(themeStoreKey);
      var hostEffects = readZustand(effectsStoreKey);
      var hostThemeState = hostTheme && hostTheme.state ? hostTheme.state : {};
      var hostEffectsState = hostEffects && hostEffects.state ? hostEffects.state : {};
      return {
        mode: allowed(source.mode || hostThemeState.theme, ['auto', 'white', 'dark'], defaults.mode),
        preset: allowed(source.preset, presetOrder.concat(['custom']), defaults.preset),
        radius: allowed(source.radius, ['default', 'none', 'sm', 'md', 'lg', 'xl'], defaults.radius),
        font: allowed(source.font, ['host', 'jetbrains-mono', 'system'], defaults.font),
        density: allowed(source.density, ['compact', 'default', 'comfortable'], defaults.density),
        contentLayout: allowed(source.contentLayout, ['full', 'centered'], defaults.contentLayout),
        customAccent: normalizeHex(source.customAccent, defaults.customAccent),
        effects: allowed(source.effects || hostEffectsState.mode, ['full', 'reduced'], defaults.effects)
      };
    }

    function cloneState(value) {
      var result = {};
      Object.keys(defaults).forEach(function (key) {
        if (Object.prototype.hasOwnProperty.call(value, key)) result[key] = value[key];
      });
      return result;
    }

    function resolvedMode() {
      if (state.mode !== 'auto') return state.mode;
      return win.matchMedia && win.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'white';
    }

    function luminance(hex) {
      var channels = hex.slice(1).match(/../g).map(function (c) {
        var n = parseInt(c, 16) / 255;
        return n <= .04045 ? n / 12.92 : Math.pow((n + .055) / 1.055, 2.4);
      });
      return channels[0] * .2126 + channels[1] * .7152 + channels[2] * .0722;
    }
    function contrastColor(hex) {
      return luminance(normalizeHex(hex, defaults.customAccent)) > .179 ? '#000000' : '#ffffff';
    }
    function applyReadableAccent() {
      var dark = resolvedMode() === 'dark';
      var accent = state.preset === 'cpamp' ? (dark ? '#74a5f8' : '#3b82f6') :
        state.preset === 'custom' ? state.customAccent : palettes[state.preset][0];
      root.style.setProperty('--action-fg', contrastColor(accent));
      // Text uses a separately adjusted accent, keeping the selected fill intact.
      var rgb = accent.slice(1).match(/../g).map(function (c) { return parseInt(c, 16); });
      var ink = accent;
      for (var i = 0; i < 40 && (dark ? luminance(ink) < .55 : luminance(ink) > .12); i++) {
        rgb = rgb.map(function (v) { return Math.round(v * .88 + (dark ? 255 : 0) * .12); });
        ink = '#' + rgb.map(function (v) { return v.toString(16).padStart(2, '0'); }).join('');
      }
      root.style.setProperty('--accent-ink', ink);
    }

    function writeZustand(key, patch) {
      var current = readZustand(key) || {};
      var currentState = current.state && typeof current.state === 'object' ? current.state : {};
      var nextState = {};
      Object.keys(currentState).forEach(function (name) {
        if (name !== '__proto__' && name !== 'constructor' && name !== 'prototype') {
          nextState[name] = currentState[name];
        }
      });
      Object.keys(patch).forEach(function (name) { nextState[name] = patch[name]; });
      storageSet(key, JSON.stringify({ state: nextState, version: Number.isInteger(current.version) ? current.version : 0 }));
    }

    function saveState() {
      storageSet(storageKey, JSON.stringify({ schema: 2, state: cloneState(state) }));
      writeZustand(themeStoreKey, {
        theme: state.mode,
        resolvedTheme: resolvedMode() === 'dark' ? 'dark' : 'light'
      });
      writeZustand(effectsStoreKey, { mode: state.effects });
    }

    var paletteProperties = [
      '--cts-primary', '--cts-secondary', '--cts-bg-light', '--cts-bg-dark', '--cts-contrast'
    ];

    function applyPalette() {
      paletteProperties.forEach(function (property) { root.style.removeProperty(property); });
      if (state.preset === 'cpamp') return;
      var values;
      if (state.preset === 'custom') {
        values = [state.customAccent, state.customAccent, '#f5f8ff', '#0b111b', contrastColor(state.customAccent)];
      } else {
        var palette = palettes[state.preset];
        values = [palette[0], palette[1], palette[2], palette[3], contrastColor(palette[0])];
      }
      values.forEach(function (value, index) { root.style.setProperty(paletteProperties[index], value); });
    }

    function applyState() {
      root.setAttribute('data-theme', resolvedMode());
      root.setAttribute('data-visual-effects', state.effects);
      root.setAttribute('data-cts-preset', state.preset);
      root.setAttribute('data-cts-radius', state.radius);
      root.setAttribute('data-cts-density', state.density);
      root.setAttribute('data-cts-font', state.font);
      root.setAttribute('data-cts-layout', state.contentLayout);
      root.setAttribute('data-cpamp-theme-studio', 'active');
      applyPalette();
      applyReadableAccent();
      saveState();
      if (isMounted()) refreshControls();
    }

    var themeCSS = String.raw`
@font-face{font-family:'JetBrains Mono';src:url('${fontRegularURL}') format('woff2');font-style:normal;font-weight:400;font-display:swap}
@font-face{font-family:'JetBrains Mono';src:url('${fontSemiBoldURL}') format('woff2');font-style:normal;font-weight:600;font-display:swap}
:root[data-cts-font='jetbrains-mono']{--cts-theme-font-family:'JetBrains Mono','PingFang SC','Microsoft YaHei',monospace}
:root[data-cts-font='system']{--cts-theme-font-family:system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif}
:root[data-cts-font='jetbrains-mono'],:root[data-cts-font='system']{--app-font-family:var(--cts-theme-font-family);--cts-studio-font-family:var(--cts-theme-font-family);font-family:var(--cts-theme-font-family)}
:root[data-cts-font='jetbrains-mono'] body,:root[data-cts-font='jetbrains-mono'] button,:root[data-cts-font='jetbrains-mono'] input,:root[data-cts-font='jetbrains-mono'] textarea,:root[data-cts-font='jetbrains-mono'] select,:root[data-cts-font='system'] body,:root[data-cts-font='system'] button,:root[data-cts-font='system'] input,:root[data-cts-font='system'] textarea,:root[data-cts-font='system'] select{font-family:var(--cts-theme-font-family)}
:root[data-cts-preset]:not([data-cts-preset='cpamp']) {
  --app-bg: var(--cts-bg-light); --app-bg-gradient:linear-gradient(125deg,color-mix(in srgb,var(--cts-primary) 9%,var(--cts-bg-light)),var(--cts-bg-light) 52%,color-mix(in srgb,var(--cts-secondary) 10%,var(--cts-bg-light)));
  --app-bg-blob-1-start:var(--cts-primary); --app-bg-blob-1-end:color-mix(in srgb,var(--cts-primary) 45%,#fff); --app-bg-blob-2-start:var(--cts-secondary); --app-bg-blob-2-end:color-mix(in srgb,var(--cts-secondary) 44%,#fff);
  --app-surface:color-mix(in srgb,var(--cts-primary) 3%,rgba(255,255,255,.94)); --app-surface-strong:color-mix(in srgb,var(--cts-primary) 2%,#fff); --app-surface-muted:color-mix(in srgb,var(--cts-primary) 5%,rgba(255,255,255,.74));
  --app-border:color-mix(in srgb,var(--cts-primary) 12%,rgba(15,23,42,.08)); --app-border-strong:color-mix(in srgb,var(--cts-primary) 18%,rgba(15,23,42,.13)); --app-accent:var(--cts-primary); --app-accent-soft:color-mix(in srgb,var(--cts-primary) 13%,transparent);
  --color-primary:var(--cts-primary); --primary-color:var(--cts-primary); --primary-hover:color-mix(in srgb,var(--cts-primary) 74%,#fff); --primary-active:color-mix(in srgb,var(--cts-primary) 80%,#000); --primary-contrast:var(--cts-contrast);
  --color-primary-light-3:color-mix(in srgb,var(--cts-primary) 72%,#fff); --color-primary-light-5:color-mix(in srgb,var(--cts-primary) 50%,#fff); --color-primary-light-7:color-mix(in srgb,var(--cts-primary) 28%,#fff); --color-primary-light-8:color-mix(in srgb,var(--cts-primary) 18%,#fff); --color-primary-light-9:color-mix(in srgb,var(--cts-primary) 9%,#fff); --color-primary-dark-2:color-mix(in srgb,var(--cts-primary) 80%,#000);
  --primary-soft:var(--color-primary-light-8); --primary-subtle:var(--color-primary-light-9); --primary-ring:color-mix(in srgb,var(--cts-primary) 24%,transparent); --border-hover:color-mix(in srgb,var(--cts-primary) 38%,transparent);
  --sidebar-bg-color:color-mix(in srgb,var(--cts-primary) 4%,rgba(255,255,255,.64)); --sidebar-logo-bg-color:color-mix(in srgb,var(--cts-primary) 5%,rgba(255,255,255,.50)); --sidebar-hover-bg-color:color-mix(in srgb,var(--cts-primary) 12%,transparent);
  --glass-bg:var(--app-surface-strong); --glass-bg-secondary:var(--app-surface-muted); --glass-border:var(--app-border-strong); --app-input-bg:color-mix(in srgb,var(--cts-primary) 3%,rgba(255,255,255,.80)); --app-input-bg-focus:var(--app-surface-strong);
}
:root[data-theme='dark'][data-cts-preset]:not([data-cts-preset='cpamp']) {
  --app-bg:var(--cts-bg-dark); --app-bg-gradient:linear-gradient(125deg,color-mix(in srgb,var(--cts-primary) 13%,var(--cts-bg-dark)),var(--cts-bg-dark) 52%,color-mix(in srgb,var(--cts-secondary) 11%,var(--cts-bg-dark)));
  --app-surface:color-mix(in srgb,var(--cts-bg-dark) 88%,#fff); --app-surface-strong:color-mix(in srgb,var(--cts-bg-dark) 94%,#fff); --app-surface-muted:color-mix(in srgb,var(--cts-bg-dark) 92%,#fff);
  --app-border:color-mix(in srgb,var(--cts-primary) 14%,rgba(255,255,255,.08)); --app-border-strong:color-mix(in srgb,var(--cts-primary) 20%,rgba(255,255,255,.13)); --sidebar-bg-color:color-mix(in srgb,var(--cts-bg-dark) 82%,transparent); --sidebar-logo-bg-color:color-mix(in srgb,var(--cts-bg-dark) 76%,transparent);
  --color-primary-light-3:color-mix(in srgb,var(--cts-primary) 82%,#fff); --color-primary-light-5:color-mix(in srgb,var(--cts-primary) 42%,var(--cts-bg-dark)); --color-primary-light-7:color-mix(in srgb,var(--cts-primary) 30%,var(--cts-bg-dark)); --color-primary-light-8:color-mix(in srgb,var(--cts-primary) 22%,var(--cts-bg-dark)); --color-primary-light-9:color-mix(in srgb,var(--cts-primary) 15%,var(--cts-bg-dark));
  --primary-active:var(--cts-primary); --primary-contrast:var(--cts-bg-dark); --glass-bg:var(--app-surface); --glass-bg-secondary:var(--app-surface-muted); --app-input-bg:var(--app-surface-strong);
}
:root[data-cts-radius='none']{--app-radius-lg:0;--app-radius-md:0;--app-radius-sm:0}
:root[data-cts-radius='sm']{--app-radius-lg:8px;--app-radius-md:5px;--app-radius-sm:3px}
:root[data-cts-radius='md']{--app-radius-lg:14px;--app-radius-md:9px;--app-radius-sm:6px}
:root[data-cts-radius='lg']{--app-radius-lg:22px;--app-radius-md:14px;--app-radius-sm:9px}
:root[data-cts-radius='xl']{--app-radius-lg:30px;--app-radius-md:20px;--app-radius-sm:13px}
:root[data-cts-density='compact']{--app-gap:14px;--app-card-padding:18px;--sidebar-width:198px;--floating-control-size:32px}
:root[data-cts-density='comfortable']{--app-gap:24px;--app-card-padding:28px;--sidebar-width:224px;--floating-control-size:38px}
@media(min-width:1280px){:root[data-cts-layout='centered'] .main-content:not(.main-content-logs):not(.main-content-plugin-resource){max-width:1440px;margin-inline:auto}}
`;

    var studioCSS = String.raw`
:host{all:initial;font-family:var(--cts-studio-font-family,var(--app-font-family,'JetBrains Mono','PingFang SC','Microsoft YaHei',monospace));color:var(--app-text-primary,#202a38)}
*,*::before,*::after{box-sizing:border-box}
.ts-stage{position:fixed;inset:0;z-index:2147483001;display:grid;grid-template-columns:1fr min(460px,100vw);height:100vh;height:100dvh;min-height:0;overflow:hidden}.ts-stage[hidden]{display:none}.ts-scrim{grid-area:1/1/2/3;border:0;background:rgba(5,10,20,.54);backdrop-filter:blur(2px);cursor:default}.ts-deck{grid-area:1/2;position:relative;display:flex;min-width:0;min-height:0;flex-direction:column;height:100%;max-height:100vh;max-height:100dvh;overflow:hidden;background:var(--app-surface-strong,#fff);border-left:1px solid var(--app-border-strong,rgba(15,23,42,.16));box-shadow:-26px 0 70px rgba(5,10,20,.22);animation:ts-enter .22s cubic-bezier(.22,1,.36,1) both}
.ts-accent{height:5px;flex:none;background:linear-gradient(90deg,var(--primary-color,#3b82f6),color-mix(in srgb,var(--primary-color,#3b82f6) 35%,#f0abfc))}.ts-head{display:flex;flex:none;align-items:flex-start;justify-content:space-between;gap:18px;padding:20px 22px 16px;border-bottom:1px solid var(--app-border,rgba(15,23,42,.09))}.ts-head h2{margin:0 0 5px;font-size:19px;line-height:1.2}.ts-head p{margin:0;color:var(--app-text-muted,#778397);font-size:12px;line-height:1.45}.ts-close{width:44px;height:44px;flex:0 0 44px;border:0;border-radius:11px;background:transparent;color:inherit;font-size:20px;cursor:pointer;touch-action:manipulation}.ts-close:hover{background:var(--app-accent-soft,rgba(59,130,246,.1))}.ts-scope{flex:none;margin:14px 22px 0;padding:9px 11px;border:1px solid var(--app-border,rgba(15,23,42,.09));border-radius:10px;background:var(--surface-subtle,rgba(15,23,42,.035));color:var(--app-text-regular,#5d6a7c);font-size:11px;line-height:1.4}
.ts-body{flex:1 1 auto;min-height:0;overflow-x:hidden;overflow-y:auto;padding:4px 22px 26px;overscroll-behavior:contain;scrollbar-gutter:stable;scrollbar-color:color-mix(in srgb,var(--primary-color,#3b82f6) 32%,var(--border-color,var(--app-border-strong,rgba(15,23,42,.16)))) transparent;scrollbar-width:thin}.ts-body::-webkit-scrollbar{width:10px;height:10px}.ts-body::-webkit-scrollbar-thumb{border:2px solid transparent;border-radius:999px;background:color-mix(in srgb,var(--primary-color,#3b82f6) 32%,var(--border-color,var(--app-border-strong,rgba(15,23,42,.16))));background-clip:padding-box}.ts-body::-webkit-scrollbar-track{background:transparent}.ts-section{padding:20px 0;border-bottom:1px solid var(--app-border,rgba(15,23,42,.09))}.ts-section:last-child{border-bottom:0}.ts-section h3{margin:0 0 12px;font-size:12px;letter-spacing:.04em;text-transform:uppercase;color:var(--app-text-regular,#5d6a7c)}.ts-grid{display:grid;gap:8px}.ts-grid-2{grid-template-columns:repeat(2,minmax(0,1fr))}.ts-grid-3{grid-template-columns:repeat(3,minmax(0,1fr))}.ts-grid-5{grid-template-columns:repeat(5,minmax(0,1fr))}.ts-button{min-width:0;min-height:44px;padding:8px;border:1px solid var(--app-border,rgba(15,23,42,.1));border-radius:11px;background:var(--app-surface-muted,rgba(255,255,255,.7));color:var(--app-text-regular,#566477);font:600 12px/1.4 var(--cts-studio-font-family,system-ui);cursor:pointer;touch-action:manipulation;transition:border-color .14s ease,background .14s ease,color .14s ease}.ts-button:hover{border-color:color-mix(in srgb,var(--primary-color,#3b82f6) 38%,transparent);color:var(--app-text-primary,#202a38)}.ts-button[aria-pressed='true']{border-color:var(--accent-ink,#245cb5);background:color-mix(in srgb,var(--primary-color,#3b82f6) 9%,var(--app-surface-strong,#fff));color:var(--accent-ink,#245cb5)}.ts-font-host{font-family:var(--app-font-family,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif)}.ts-font-jetbrains{font-family:'JetBrains Mono','PingFang SC','Microsoft YaHei',monospace}.ts-font-system{font-family:system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif}
.ts-preset{display:grid;grid-template-columns:34px minmax(0,1fr);align-items:center;gap:9px;min-height:56px;text-align:left}.ts-swatches{display:flex;width:34px;height:30px;overflow:hidden;border:1px solid rgba(127,127,127,.22);border-radius:8px}.ts-swatches i{flex:1}.ts-preset span:last-child{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.ts-custom{grid-column:1/-1;display:flex;align-items:center;justify-content:space-between;gap:12px;padding:8px 10px}.ts-custom input{width:44px;height:44px;padding:2px;border:1px solid var(--app-border-strong,rgba(15,23,42,.15));border-radius:8px;background:transparent;cursor:pointer}.ts-radius{display:block;width:26px;height:18px;margin:0 auto 5px;border:2px solid currentColor}.ts-radius-sm{border-radius:3px}.ts-radius-md{border-radius:6px}.ts-radius-lg{border-radius:10px}.ts-radius-xl{border-radius:15px}.ts-layout{min-height:62px}.ts-footer{flex:none;padding:13px 22px;border-top:1px solid var(--app-border,rgba(15,23,42,.09))}.ts-reset{width:100%;min-height:44px}.ts-button:focus-visible,.ts-close:focus-visible,input:focus-visible{outline:2px solid var(--primary-color,#3b82f6);outline-offset:2px}
@keyframes ts-enter{from{opacity:.5;transform:translateX(28px)}}@media(max-width:560px){.ts-stage{grid-template-columns:1fr}.ts-scrim,.ts-deck{grid-area:1/1}.ts-deck{border-left:0}.ts-head{padding:17px 16px 14px}.ts-scope{margin-inline:16px}.ts-body{padding-inline:16px}.ts-footer{padding:11px 16px}.ts-grid-2{grid-template-columns:1fr}.ts-grid-5{grid-template-columns:repeat(3,minmax(0,1fr))}}@media(prefers-reduced-motion:reduce){.ts-deck{animation:none}}
`;

    function ensureThemeStyle() {
      var style = doc.getElementById(styleID);
      if (!style) {
        style = doc.createElement('style');
        style.id = styleID;
        (doc.head || root).appendChild(style);
      }
      if (style.textContent !== themeCSS) style.textContent = themeCSS;
      return style;
    }

    function paletteName(value) {
      var names = {
        cpamp: 'pCpamp', 'ember-dusk': 'pEmber', 'jade-circuit': 'pJade', 'coral-mist': 'pCoral',
        'glacier-signal': 'pGlacier', 'solar-flare': 'pSolar', 'moss-stone': 'pMoss',
        'deep-current': 'pDeep', 'amethyst-fog': 'pAmethyst', graphite: 'pGraphite'
      };
      return tr(names[value]);
    }

    function swatches(value) {
      if (value === 'cpamp') return ['#3b82f6', '#67d4c2'];
      return [palettes[value][0], palettes[value][1]];
    }

    function button(action, value, label, extra, inner) {
      return '<button type="button" class="ts-button ' + (extra || '') + '" data-action="' + action + '" data-value="' + value + '">' + (inner || '') + '<span>' + label + '</span></button>';
    }

    function presetButton(value) {
      var colors = swatches(value);
      return button('preset', value, paletteName(value), 'ts-preset', '<span class="ts-swatches"><i style="background:' + colors[0] + '"></i><i style="background:' + colors[1] + '"></i></span>');
    }

    function markup() {
      var presets = presetOrder.map(presetButton).join('');
      return '<div class="ts-stage" hidden><button type="button" class="ts-scrim" aria-label="' + tr('close') + '"></button><section class="ts-deck" role="dialog" aria-modal="true" aria-labelledby="ts-title"><div class="ts-accent"></div>' +
        '<header class="ts-head"><div><h2 id="ts-title">' + tr('title') + '</h2><p>' + tr('subtitle') + '</p></div><button type="button" class="ts-close" aria-label="' + tr('close') + '">×</button></header>' +
        '<div class="ts-scope">' + tr(hostConnected ? 'scopeHost' : 'scopeFrame') + '</div><div class="ts-body">' +
        '<section class="ts-section"><h3>' + tr('mode') + '</h3><div class="ts-grid ts-grid-3">' + button('mode','auto',tr('auto')) + button('mode','white',tr('light')) + button('mode','dark',tr('dark')) + '</div></section>' +
        '<section class="ts-section"><h3>' + tr('palette') + '</h3><div class="ts-grid ts-grid-2">' + presets + '<label class="ts-button ts-custom" data-custom-label><span>' + tr('custom') + '</span><input type="color" data-action="custom" aria-label="' + tr('custom') + '"></label></div></section>' +
        '<section class="ts-section"><h3>' + tr('radius') + '</h3><div class="ts-grid ts-grid-5">' +
        button('radius','default',tr('normal'),'','<i class="ts-radius ts-radius-md"></i>') + button('radius','none',tr('square'),'','<i class="ts-radius"></i>') + button('radius','sm',tr('small'),'','<i class="ts-radius ts-radius-sm"></i>') + button('radius','md',tr('medium'),'','<i class="ts-radius ts-radius-md"></i>') + button('radius','lg',tr('large'),'','<i class="ts-radius ts-radius-lg"></i>') + button('radius','xl',tr('xlarge'),'','<i class="ts-radius ts-radius-xl"></i>') + '</div></section>' +
        '<section class="ts-section"><h3>' + tr('font') + '</h3><div class="ts-grid ts-grid-3">' + button('font','host',tr('hostFont'),'ts-font-host') + button('font','jetbrains-mono',tr('jetBrainsMono'),'ts-font-jetbrains') + button('font','system',tr('systemSans'),'ts-font-system') + '</div></section>' +
        '<section class="ts-section"><h3>' + tr('density') + '</h3><div class="ts-grid ts-grid-3">' + button('density','compact',tr('compact')) + button('density','default',tr('normal')) + button('density','comfortable',tr('comfortable')) + '</div></section>' +
        '<section class="ts-section"><h3>' + tr('layout') + '</h3><div class="ts-grid ts-grid-2">' + button('contentLayout','full',tr('full'),'ts-layout') + button('contentLayout','centered',tr('centered'),'ts-layout') + '</div></section>' +
        '<section class="ts-section"><h3>' + tr('effects') + '</h3><div class="ts-grid ts-grid-2">' + button('effects','full',tr('rich')) + button('effects','reduced',tr('efficient')) + '</div></section>' +
        '</div><footer class="ts-footer"><button type="button" class="ts-button ts-reset" data-action="reset">↺ ' + tr('reset') + '</button></footer></section></div>';
    }

    function refreshControls() {
      if (!isMounted()) return;
      Array.prototype.forEach.call(shadow.querySelectorAll('[data-action][data-value]'), function (control) {
        var active = state[control.getAttribute('data-action')] === control.getAttribute('data-value');
        control.setAttribute('aria-pressed', active ? 'true' : 'false');
      });
      var customLabel = shadow.querySelector('[data-custom-label]');
      if (customLabel) customLabel.setAttribute('aria-pressed', state.preset === 'custom' ? 'true' : 'false');
      var colorInput = shadow.querySelector('input[data-action="custom"]');
      if (colorInput) colorInput.value = state.customAccent;
    }

    function isMounted() {
      return Boolean(
        mount && mount.isConnected && shadow && shadow.host === mount &&
        doc.getElementById(mountID) === mount
      );
    }

    function stageElement() {
      return isMounted() ? shadow.querySelector('.ts-stage') : null;
    }

    function isOpen() {
      var stage = stageElement();
      return Boolean(stage && !stage.hidden);
    }

    function normalizedControlLabel(value) {
      return String(value || '').trim().replace(/\s+/g, ' ').toLowerCase();
    }

    function isHostThemeControl(control) {
      if (!control || control.closest('#' + mountID)) return false;
      if (control.getAttribute('data-cpamp-theme-studio-trigger') === 'true') return true;
      var labels = [control.getAttribute('aria-label'), control.getAttribute('title')];
      for (var index = 0; index < labels.length; index += 1) {
        if (hostThemeLabels[normalizedControlLabel(labels[index])]) return true;
      }
      return false;
    }

    function findHostThemeControl() {
      // CPAMP renders the login-page toolbar as a plain div, while authenticated
      // layouts use a header-like action row. Match the accessible label instead
      // of relying on a particular container so both surfaces keep working.
      var controls = doc.querySelectorAll('button');
      for (var index = 0; index < controls.length; index += 1) {
        if (isHostThemeControl(controls[index])) return controls[index];
      }
      return null;
    }

    function syncHostThemeControl() {
      if (!hostThemeControl || !hostThemeControl.isConnected) return;
      hostThemeControl.setAttribute('data-cpamp-theme-studio-trigger', 'true');
      hostThemeControl.setAttribute('title', tr('open'));
      hostThemeControl.setAttribute('aria-label', tr('open'));
      hostThemeControl.setAttribute('aria-haspopup', 'dialog');
      hostThemeControl.setAttribute('aria-expanded', isOpen() ? 'true' : 'false');
    }

    function bindHostThemeControl() {
      var control = findHostThemeControl();
      if (!control) {
        hostThemeControl = null;
        return false;
      }
      hostThemeControl = control;
      syncHostThemeControl();
      return true;
    }

    function lockBodyScroll() {
      if (!doc.body) return;
      if (overflowOwner === doc.body) {
        doc.body.style.overflow = 'hidden';
        return;
      }
      releaseBodyScroll();
      overflowOwner = doc.body;
      previousOverflow = overflowOwner.style.overflow;
      overflowOwner.style.overflow = 'hidden';
    }

    function releaseBodyScroll() {
      if (!overflowOwner) return;
      overflowOwner.style.overflow = previousOverflow;
      overflowOwner = null;
      previousOverflow = '';
    }

    function revealStage(focusOrigin) {
      var stage = stageElement();
      if (!stage) return false;
      if (stage.hidden) {
        returnFocus = focusOrigin && focusOrigin.isConnected ? focusOrigin : doc.activeElement;
        stage.hidden = false;
      }
      lockBodyScroll();
      var app = doc.getElementById('app-shell');
      if (app) app.inert = true;
      syncHostThemeControl();
      var closeButton = shadow.querySelector('.ts-close');
      if (closeButton) closeButton.focus();
      return true;
    }

    function open(focusOrigin) {
      openRequested = true;
      ensureThemeStyle();
      if (!ensureMounted()) {
        scheduleEnsure();
        return;
      }
      revealStage(focusOrigin);
    }

    function close() {
      openRequested = false;
      var stage = stageElement();
      if (stage) stage.hidden = true;
      var app = doc.getElementById('app-shell');
      if (app) app.inert = false;
      releaseBodyScroll();
      syncHostThemeControl();

      var focusTarget = returnFocus && returnFocus.isConnected ? returnFocus :
        (hostThemeControl && hostThemeControl.isConnected ? hostThemeControl : null);
      returnFocus = null;
      if (focusTarget && typeof focusTarget.focus === 'function') focusTarget.focus();
    }

    function reset() {
      state = cloneState(defaults);
      applyState();
    }

    function findInEventPath(event, selector) {
      var path = event.composedPath ? event.composedPath() : [];
      for (var index = 0; index < path.length; index += 1) {
        var candidate = path[index];
        if (candidate && typeof candidate.matches === 'function' && candidate.matches(selector)) return candidate;
      }
      return event.target && event.target.closest ? event.target.closest(selector) : null;
    }

    function handleClick(event) {
      var control = findInEventPath(event, 'button');
      if (!control) return;
      if (control.classList.contains('ts-close') || control.classList.contains('ts-scrim')) return close();
      var action = control.getAttribute('data-action');
      if (action === 'reset') return reset();
      var value = control.getAttribute('data-value');
      if (!action || !value || !Object.prototype.hasOwnProperty.call(state, action)) return;
      state[action] = value;
      applyState();
    }

    function handleHostControlClick(event) {
      var control = findInEventPath(event, 'button');
      if (!isHostThemeControl(control)) return;
      hostThemeControl = control;
      syncHostThemeControl();
      event.preventDefault();
      event.stopImmediatePropagation();
      open(control);
    }

    function handleKey(event) {
      if (!isOpen()) return;
      if (event.key === 'Escape') {
        event.preventDefault();
        close();
        return;
      }
      if (event.key !== 'Tab') return;
      var controls = Array.prototype.slice.call(shadow.querySelectorAll('.ts-deck button:not([disabled]),.ts-deck input:not([disabled])'));
      if (!controls.length) return;
      var first = controls[0];
      var last = controls[controls.length - 1];
      if (event.shiftKey && shadow.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && shadow.activeElement === last) { event.preventDefault(); first.focus(); }
    }

    function publishRuntime() {
      [win, root, mount].forEach(function (target) {
        if (!target) return;
        try {
          Object.defineProperty(target, '__SOFT_THEME_STUDIO__', {
            configurable: true,
            enumerable: false,
            writable: true,
            value: runtime
          });
        } catch (_) {
          try { target.__SOFT_THEME_STUDIO__ = runtime; } catch (_) { /* read-only host */ }
        }
      });
    }

    function ensureMounted() {
      if (!doc.body) return false;
      if (isMounted()) {
        bindHostThemeControl();
        publishRuntime();
        return true;
      }

      releaseBodyScroll();
      var existing = doc.getElementById(mountID);
      if (existing) existing.remove();

      mount = doc.createElement('div');
      mount.id = mountID;
      mount.setAttribute('data-version', runtimeVersion);
      doc.body.appendChild(mount);
      shadow = mount.attachShadow({ mode: 'open' });
      var style = doc.createElement('style');
      style.textContent = studioCSS;
      shadow.appendChild(style);
      var shell = doc.createElement('div');
      shell.innerHTML = markup();
      shadow.appendChild(shell);
      shadow.addEventListener('click', handleClick);
      shadow.addEventListener('input', function (event) {
        var input = findInEventPath(event, 'input[data-action="custom"]');
        if (input) {
          state.customAccent = normalizeHex(input.value, state.customAccent);
          state.preset = 'custom';
          applyState();
        }
      });
      bindHostThemeControl();
      publishRuntime();
      refreshControls();
      if (openRequested) revealStage();
      return true;
    }

    function scheduleEnsure() {
      if (remountScheduled) return;
      remountScheduled = true;
      var schedule = win.requestAnimationFrame || function (callback) { return win.setTimeout(callback, 0); };
      schedule(function () {
        remountScheduled = false;
        ensureThemeStyle();
        ensureMounted();
        bindHostThemeControl();
      });
    }

    function watchHostDOM() {
      if (observer || !win.MutationObserver || !root) return;
      observer = new win.MutationObserver(function () {
        scheduleEnsure();
      });
      observer.observe(root, { childList: true, subtree: true });
    }

    var runtime = {
      version: runtimeVersion,
      ensure: function () {
        ensureThemeStyle();
        var ready = ensureMounted();
        bindHostThemeControl();
        publishRuntime();
        return ready;
      },
      open: open,
      close: close,
      reset: reset,
      getState: function () { return cloneState(state); },
      getDebugState: function () {
        return {
          mounted: isMounted(),
          open: isOpen(),
          mountCount: doc.querySelectorAll('#' + mountID).length,
          overflowLocked: Boolean(overflowOwner),
          stageCount: isMounted() ? shadow.querySelectorAll('.ts-stage').length : 0,
          hostControlCount: doc.querySelectorAll('[data-cpamp-theme-studio-trigger="true"]').length,
          hostControlBound: Boolean(hostThemeControl && hostThemeControl.isConnected),
        };
      }
    };

    publishRuntime();
    ensureThemeStyle();
    applyState();
    if (doc.body) ensureMounted();
    else doc.addEventListener('DOMContentLoaded', ensureMounted, { once: true });
    doc.addEventListener('click', handleHostControlClick, true);
    doc.addEventListener('keydown', handleKey, true);
    watchHostDOM();

    if (win.matchMedia) {
      var colorScheme = win.matchMedia('(prefers-color-scheme: dark)');
      var onSchemeChange = function () { if (state.mode === 'auto') applyState(); };
      if (colorScheme.addEventListener) colorScheme.addEventListener('change', onSchemeChange);
      else if (colorScheme.addListener) colorScheme.addListener(onSchemeChange);
    }

    publishRuntime();
  }
})();
