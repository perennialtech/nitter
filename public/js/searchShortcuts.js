// SPDX-License-Identifier: AGPL-3.0-only

const searchInputs = '.search-field input[name="q"], .search-bar input[name="q"], .nav-search input[name="q"]';

document.addEventListener("keydown", event => {
  if (event.defaultPrevented || event.isComposing || event.ctrlKey || event.altKey || event.metaKey) return;
  const active = document.activeElement;
  if (event.key === "Escape" && active?.matches(searchInputs)) {
    active.blur();
    event.preventDefault();
    return;
  }
  if (event.key !== "/" || active?.matches("input, textarea, select") || active?.isContentEditable) return;

  const input = document.querySelector('.search-field input[name="q"], .search-bar input[name="q"]')
    || document.querySelector('.nav-search input[name="q"]');
  if (input) {
    input.focus();
    input.select();
    event.preventDefault();
  }
});
