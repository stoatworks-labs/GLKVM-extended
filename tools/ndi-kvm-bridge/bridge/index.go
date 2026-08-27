package main

// indexHTML is the browser UI: an MJPEG <img> plus JS that captures pointer and
// keyboard events, normalizes coordinates, maps keys to X11 keysyms, and POSTs
// them to /input. %d/%d are the source width/height (for aspect only).
const indexHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>NDI KVM</title>
<style>
  html,body{margin:0;height:100%%;background:#0b0d10;color:#ddd;font-family:system-ui}
  .bar{height:34px;display:flex;align-items:center;gap:12px;padding:0 12px;background:#1b1f27;font-size:13px}
  .dot{width:8px;height:8px;border-radius:50%%;background:#f0a020}
  .dot.ok{background:#67c23a}
  .wrap{position:absolute;inset:34px 0 0 0;display:flex;align-items:center;justify-content:center}
  img{max-width:100%%;max-height:100%%;cursor:none;outline:none;image-rendering:auto}
  .hint{color:#8a93a2}
</style></head>
<body>
  <div class="bar"><span id="dot" class="dot"></span><b>NDI&nbsp;KVM</b>
    <span class="hint">click the image to grab keyboard · %dx%d</span>
    <span id="stat" class="hint" style="margin-left:auto"></span></div>
  <div class="wrap"><img id="v" src="/stream" tabindex="0" draggable="false"></div>
<script>
const v = document.getElementById('v'), dot=document.getElementById('dot'), stat=document.getElementById('stat');
v.addEventListener('load', ()=>{ dot.classList.add('ok'); });
let sending=0;
function post(ev){
  sending++;
  fetch('/input',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(ev),keepalive:true})
    .catch(()=>{}).finally(()=>{sending--; stat.textContent = sending>4?'busy':''; });
}
function norm(e){
  const r=v.getBoundingClientRect();
  let x=(e.clientX-r.left)/r.width, y=(e.clientY-r.top)/r.height;
  x=Math.min(1,Math.max(0,x)); y=Math.min(1,Math.max(0,y));
  return {x,y};
}
// throttle mouse moves to ~60/s
let lastMove=0;
v.addEventListener('mousemove',e=>{
  const now=performance.now(); if(now-lastMove<16) return; lastMove=now;
  const p=norm(e); post({t:'move',x:p.x,y:p.y});
});
v.addEventListener('mousedown',e=>{ e.preventDefault(); v.focus(); const p=norm(e); post({t:'down',x:p.x,y:p.y,button:e.button}); });
window.addEventListener('mouseup',e=>{ post({t:'up',button:e.button}); });
v.addEventListener('contextmenu',e=>e.preventDefault());
v.addEventListener('wheel',e=>{ e.preventDefault(); post({t:'wheel',dx:Math.sign(e.deltaX),dy:-Math.sign(e.deltaY)}); },{passive:false});

// --- keyboard: map to X11 keysyms ---
const SPECIAL={
  'Enter':0xFF0D,'Backspace':0xFF08,'Tab':0xFF09,'Escape':0xFF1B,'Delete':0xFFFF,
  'ArrowLeft':0xFF51,'ArrowUp':0xFF52,'ArrowRight':0xFF53,'ArrowDown':0xFF54,
  'Home':0xFF50,'End':0xFF57,'PageUp':0xFF55,'PageDown':0xFF56,'Insert':0xFF63,
  ' ':0x20,'Shift':0xFFE1,'Control':0xFFE3,'Alt':0xFFE9,'Meta':0xFFE7,'CapsLock':0xFFE5,
  'F1':0xFFBE,'F2':0xFFBF,'F3':0xFFC0,'F4':0xFFC1,'F5':0xFFC2,'F6':0xFFC3,
  'F7':0xFFC4,'F8':0xFFC5,'F9':0xFFC6,'F10':0xFFC7,'F11':0xFFC8,'F12':0xFFC9
};
function keysym(e){
  if(SPECIAL[e.key]!==undefined) return SPECIAL[e.key];
  if(e.key.length===1){ const c=e.key.codePointAt(0); return c<=0xFF? c : 0x1000000+c; }
  return 0;
}
v.addEventListener('keydown',e=>{ const k=keysym(e); if(k){ e.preventDefault(); post({t:'kdown',keysym:k}); } });
v.addEventListener('keyup',  e=>{ const k=keysym(e); if(k){ e.preventDefault(); post({t:'kup',keysym:k}); } });
</script>
</body></html>`
