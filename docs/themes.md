# Themes

WinTUI ships with eight curated color palettes. Each one has matching
**Dark** and **Light** variants — WinTUI detects your terminal's
background at startup and picks the variant that reads correctly,
so the theme works the same whether your terminal is dark or light
without any extra configuration.

![Cycling through every WinTUI theme](themes/cycle.gif)

## Switching themes

Open **Settings** (tab `4`), arrow down to **Color Theme**, then use
`←` / `→` (or `Space`) to cycle. Changes apply live — no restart, no
config file edit. The picker also exposes **Theme Background**, an
opt-in setting that paints your terminal background with the active
palette's color via OSC 11 (default: leave the terminal alone).

The active theme is stored in `%APPDATA%\wintui\settings.json` under
the `theme` key, so you can also set it from a config sync script or
deployment.

## The palettes

### Sweet Pink — default

WinTUI's original palette: pink and lavender structure with a mint
accent for staged intent. The hue system the rest of the app was
designed around.

![Sweet Pink theme](themes/sweet-pink.png)

### WinTUI Midnight

App-native dark theme — close enough to the default to keep the
WinTUI identity, but calmer and more balanced for daily use.

![WinTUI Midnight theme](themes/wintui-midnight.png)

### Catppuccin

The Mocha and Latte variants from the published Catppuccin spec.

![Catppuccin theme](themes/catppuccin.png)

### Nord

Frost blues over polar night — the classic Nord spec with a contrast-
matched light companion.

![Nord theme](themes/nord.png)

### Dracula

Bold pink, purple, and cyan from the published Dracula spec.

![Dracula theme](themes/dracula.png)

### Tokyo Night

Muted blue and purple from the Tokyo Night spec, with the Day variant
available automatically on light terminals.

![Tokyo Night theme](themes/tokyo-night.png)

### Ember

Warm earth tones — burnt amber, copper, sage. Dusk for dark
terminals, Dawn for light ones (autumn paper, burnt-sienna ink,
forest sage).

![Ember theme](themes/ember.png)

### Monochrome

High-contrast greyscale for accessibility. Bold weight carries the
emphasis color usually provides; success / danger / warning keep a
small hue cue so status glyphs stay legible.

![Monochrome theme](themes/monochrome.png)

## Theme Background

A second Settings row, **Theme Background**, controls whether WinTUI
paints the terminal background with the active palette's `Background`
color.

- `terminal` (default) — leave your terminal's background untouched.
- `theme` — tint with the palette's background for an immersive look.
  Recommended on Catppuccin, Nord, Dracula, Tokyo Night, Ember, and
  Monochrome.

OSC 11 support varies by terminal, so on unsupported terminals this
setting is a no-op. The default Sweet Pink theme doesn't define a
background, so enabling Theme Background without first picking a
different theme is also a graceful no-op.

---

Back to [README](../README.md).
