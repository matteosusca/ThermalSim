// ThermalSim Web UI Controller

const canvas = document.getElementById('paper-canvas');
const ctx = canvas.getContext('2d');
const paperSheet = document.getElementById('paper-sheet');
const scrollBox = document.getElementById('paper-scroll-box');
const logContainer = document.getElementById('fsm-event-log');

let currentRows = 0;
let zoomLevel = 1;

// Image buffer to hold full receipt dots
let paperData = []; // Array of [384]Uint8Array
let pendingRows = [];
let renderScheduled = false;

function initCanvas() {
  canvas.width = 384;
  canvas.height = 200;
  ctx.fillStyle = '#fafaf9';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
}
initCanvas();

// Connect Server-Sent Events (SSE) stream
function connectStream() {
  const evtSource = new EventSource('/api/stream');

  evtSource.addEventListener('row', (e) => {
    const data = JSON.parse(e.data);
    queueRow(data.index, data.dots);
  });

  evtSource.addEventListener('clear', () => {
    clearCanvas();
  });

  evtSource.addEventListener('fsm', (e) => {
    const data = JSON.parse(e.data);
    updateFSM(data);
  });

  evtSource.addEventListener('status', (e) => {
    const data = JSON.parse(e.data);
    updateSensors(data);
  });

  evtSource.onerror = () => {
    console.warn("SSE connection interrupted, retrying in 2s...");
    evtSource.close();
    setTimeout(connectStream, 2000);
  };
}

function queueRow(rowIndex, dotsHex) {
  // Decode dots from hex string or byte array
  const rowDots = new Uint8Array(384);
  if (typeof dotsHex === 'string') {
    for (let i = 0; i < 384 && i < dotsHex.length / 2; i++) {
      rowDots[i] = parseInt(dotsHex.substr(i * 2, 2), 16);
    }
  } else if (Array.isArray(dotsHex)) {
    for (let i = 0; i < 384 && i < dotsHex.length; i++) {
      rowDots[i] = dotsHex[i];
    }
  }

  // Store in memory
  while (paperData.length <= rowIndex) {
    paperData.push(null);
  }
  paperData[rowIndex] = rowDots;
  currentRows = paperData.length;

  // Add to batch queue
  pendingRows.push({ index: rowIndex, dots: rowDots });
  scheduleRender();
}

function scheduleRender() {
  if (renderScheduled) return;
  renderScheduled = true;
  requestAnimationFrame(flushPendingRows);
}

function flushPendingRows() {
  renderScheduled = false;
  if (pendingRows.length === 0) return;

  const rowsToDraw = pendingRows;
  pendingRows = [];

  // 1. Calculate maximum row index to ensure canvas has adequate height
  let maxIndex = canvas.height - 1;
  for (let i = 0; i < rowsToDraw.length; i++) {
    if (rowsToDraw[i].index > maxIndex) {
      maxIndex = rowsToDraw[i].index;
    }
  }

  const neededHeight = maxIndex + 1;
  if (neededHeight > canvas.height) {
    const newHeight = Math.max(neededHeight + 100, Math.floor(canvas.height * 1.5), 200);
    const tempCanvas = document.createElement('canvas');
    tempCanvas.width = canvas.width;
    tempCanvas.height = canvas.height;
    tempCanvas.getContext('2d').drawImage(canvas, 0, 0);

    canvas.height = newHeight;
    ctx.fillStyle = '#fafaf9';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.drawImage(tempCanvas, 0, 0);
  }

  // 2. Blit all pending scanlines directly to the canvas
  for (let r = 0; r < rowsToDraw.length; r++) {
    const item = rowsToDraw[r];
    const rowDots = item.dots;
    const imgData = ctx.createImageData(384, 1);
    for (let x = 0; x < 384; x++) {
      const val = rowDots[x];
      const pIdx = x * 4;
      if (val > 0) {
        const shade = 255 - val;
        imgData.data[pIdx] = shade;
        imgData.data[pIdx + 1] = shade;
        imgData.data[pIdx + 2] = shade;
        imgData.data[pIdx + 3] = 255;
      } else {
        imgData.data[pIdx] = 250;
        imgData.data[pIdx + 1] = 250;
        imgData.data[pIdx + 2] = 249;
        imgData.data[pIdx + 3] = 255;
      }
    }
    ctx.putImageData(imgData, 0, item.index);
  }

  // 3. Auto-scroll smoothly to the bottom of the paper roll
  scrollBox.scrollTop = scrollBox.scrollHeight;
}

function clearCanvas() {
  paperData = [];
  pendingRows = [];
  currentRows = 0;
  initCanvas();
}

function updateFSM(data) {
  document.getElementById('fsm-state-badge').textContent = data.state;

  // Append to logic analyzer log
  const item = document.createElement('div');
  item.className = 'log-item';
  const hex = '0x' + data.byte.toString(16).toUpperCase().padStart(2, '0');
  item.innerHTML = `<span class="log-hex">${hex}</span> <span class="log-desc">${data.desc}</span>`;
  logContainer.insertBefore(item, logContainer.firstChild);

  // Limit log entries
  while (logContainer.children.length > 50) {
    logContainer.removeChild(logContainer.lastChild);
  }
}

function updateSensors(data) {
  // Update LED
  const led = document.getElementById('status-led');
  const ledLabel = document.getElementById('led-label');
  led.className = 'led';
  if (data.led === 1) {
    led.classList.add('led-on');
    ledLabel.textContent = 'LED: Normal (1 blink)';
  } else if (data.led === 2) {
    led.classList.add('led-error');
    ledLabel.textContent = 'LED: Platen Open (2 blinks)';
  } else if (data.led === 3) {
    led.classList.add('led-error');
    ledLabel.textContent = 'LED: Paper Out (3 blinks)';
  } else if (data.led === 5) {
    led.classList.add('led-error');
    ledLabel.textContent = 'LED: Overheat >60°C (5 blinks)';
  }

  // Update Temp & Voltage labels
  document.getElementById('val-temp').textContent = data.temperature.toFixed(1) + ' °C';
  document.getElementById('val-voltage').textContent = data.voltage.toFixed(1) + ' V';
  document.getElementById('slider-temp').value = data.temperature;
  document.getElementById('slider-voltage').value = data.voltage;

  // Buffer stats
  const fillPct = (data.bufferLen / data.bufferCap) * 100;
  document.getElementById('buffer-fill-bar').style.width = fillPct + '%';
  document.getElementById('buffer-label').textContent = `${data.bufferLen} / ${data.bufferCap} bytes`;
  document.getElementById('drops-label').textContent = `Dropped: ${data.droppedBytes} bytes`;
}

// Controls & Event Listeners
document.getElementById('btn-clear').addEventListener('click', () => {
  fetch('/api/clear', { method: 'POST' });
});

document.getElementById('btn-zoom-1x').addEventListener('click', () => {
  zoomLevel = 1;
  if (paperSheet) {
    paperSheet.style.transform = 'scale(1)';
    paperSheet.style.transformOrigin = 'top center';
  }
  document.getElementById('btn-zoom-1x').classList.add('active');
  document.getElementById('btn-zoom-2x').classList.remove('active');
});

document.getElementById('btn-zoom-2x').addEventListener('click', () => {
  zoomLevel = 2;
  if (paperSheet) {
    paperSheet.style.transform = 'scale(1.4)';
    paperSheet.style.transformOrigin = 'top center';
  }
  document.getElementById('btn-zoom-2x').classList.add('active');
  document.getElementById('btn-zoom-1x').classList.remove('active');
});

// Virtual Hardware Buttons
document.getElementById('btn-feed').addEventListener('click', () => {
  fetch('/api/feed', { method: 'POST' });
});

let powerOn = true;
document.getElementById('btn-power').addEventListener('click', () => {
  powerOn = !powerOn;
  const btn = document.getElementById('btn-power');
  if (powerOn) {
    btn.className = 'btn-panel btn-power-on';
    btn.textContent = 'POWER: ON';
    fetch('/api/power', { method: 'POST', body: JSON.stringify({ power: true }) });
  } else {
    btn.className = 'btn-panel btn-power-off';
    btn.textContent = 'POWER: OFF';
    fetch('/api/power', { method: 'POST', body: JSON.stringify({ power: false }) });
  }
});

// Fault Toggles
document.querySelectorAll('input[name="paper-state"]').forEach((radio) => {
  radio.addEventListener('change', (e) => {
    fetch('/api/faults', {
      method: 'POST',
      body: JSON.stringify({ paperState: parseInt(e.target.value) })
    });
  });
});

let platenOpen = false;
document.getElementById('btn-platen').addEventListener('click', () => {
  platenOpen = !platenOpen;
  const btn = document.getElementById('btn-platen');
  btn.textContent = platenOpen ? 'Cover: OPEN (Halted)' : 'Cover: Closed';
  btn.style.color = platenOpen ? '#f87171' : '#e5e7eb';
  fetch('/api/faults', {
    method: 'POST',
    body: JSON.stringify({ platenOpen: platenOpen })
  });
});

document.getElementById('slider-temp').addEventListener('input', (e) => {
  const val = parseFloat(e.target.value);
  document.getElementById('val-temp').textContent = val.toFixed(1) + ' °C';
  fetch('/api/faults', {
    method: 'POST',
    body: JSON.stringify({ temperature: val })
  });
});

document.getElementById('slider-voltage').addEventListener('input', (e) => {
  const val = parseFloat(e.target.value);
  document.getElementById('val-voltage').textContent = val.toFixed(1) + ' V';
  fetch('/api/faults', {
    method: 'POST',
    body: JSON.stringify({ voltage: val })
  });
});

// Test Payload Injectors
document.getElementById('btn-sample-receipt').addEventListener('click', () => {
  fetch('/api/inject', { method: 'POST', body: JSON.stringify({ test: 'sample' }) });
});

document.getElementById('btn-selftest').addEventListener('click', () => {
  fetch('/api/inject', { method: 'POST', body: JSON.stringify({ test: 'selftest' }) });
});

document.getElementById('btn-quirk-lockup').addEventListener('click', () => {
  fetch('/api/inject', { method: 'POST', body: JSON.stringify({ test: 'quirk_lockup' }) });
});

document.getElementById('btn-quirk-barcode').addEventListener('click', () => {
  fetch('/api/inject', { method: 'POST', body: JSON.stringify({ test: 'quirk_barcode' }) });
});

document.getElementById('btn-quirk-overflow').addEventListener('click', () => {
  fetch('/api/inject', { method: 'POST', body: JSON.stringify({ test: 'quirk_overflow' }) });
});

// Start streaming on load
connectStream();
