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
