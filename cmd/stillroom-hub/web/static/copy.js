// One button, one job. Clipboard access can be denied; say so rather than
// silently doing nothing.
document.addEventListener('click', function (e) {
  var btn = e.target.closest('[data-copy]');
  if (!btn) return;
  navigator.clipboard.writeText(btn.getAttribute('data-copy')).then(function () {
    var was = btn.textContent;
    btn.textContent = 'Copied';
    btn.classList.add('done');
    setTimeout(function () { btn.textContent = was; btn.classList.remove('done'); }, 1600);
  }, function () {
    btn.textContent = 'Press ⌘C';
  });
});


// Share-mode toggle on the landing page: swap which command is shown and
// which one the Copy button copies. Nothing here talks to a server — the
// choice rides along in the pasted text as --knowledge or --full.
(function () {
  var box = document.querySelector('.command[data-mode]');
  if (!box) return;

  function show(mode) {
    box.setAttribute('data-mode', mode);
    box.querySelectorAll('.mode-opt').forEach(function (b) {
      var on = b.getAttribute('data-mode') === mode;
      b.classList.toggle('on', on);
      b.setAttribute('aria-selected', on ? 'true' : 'false');
    });
    box.querySelector('.cmd-knowledge').hidden = mode !== 'knowledge';
    box.querySelector('.cmd-full').hidden = mode !== 'full';
    box.querySelector('.mode-note-knowledge').hidden = mode !== 'knowledge';
    box.querySelector('.mode-note-full').hidden = mode !== 'full';
  }

  box.querySelectorAll('.mode-opt').forEach(function (b) {
    b.addEventListener('click', function () { show(b.getAttribute('data-mode')); });
  });

  var copy = box.querySelector('[data-copy-mode]');
  copy.addEventListener('click', function () {
    var mode = box.getAttribute('data-mode');
    var text = box.querySelector(mode === 'full' ? '.cmd-full' : '.cmd-knowledge').textContent;
    navigator.clipboard.writeText(text).then(function () {
      var was = copy.textContent;
      copy.textContent = 'Copied'; copy.classList.add('done');
      setTimeout(function () { copy.textContent = was; copy.classList.remove('done'); }, 1600);
    }, function () { copy.textContent = 'Press ⌘C'; });
  });
})();
