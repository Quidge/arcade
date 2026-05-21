// Scribble a drawing onto the game's <canvas> by dispatching synthetic pointer
// events. Pass this whole function to the chrome-devtools `evaluate_script` tool.
//
// The drawing surface only responds to pointer events, so fill()/click() cannot
// draw on it. Edit the STROKES constant below before each call.
//
// Coordinates are in CSS pixels relative to the canvas's top-left corner. The
// canvas is square; query its on-screen size first if unsure:
//   document.querySelector('canvas').getBoundingClientRect()
// Strokes drawn off the visible area are simply clipped — keep points within
// roughly 0..(canvas display width).
//
// A stroke is an array of [x, y] points; the pen is down for the whole stroke.

() => {
  // ---- EDIT THIS: one array per pen-stroke -------------------------------
  // Example: a quick stick figure. Replace with strokes that suit the prompt.
  const STROKES = [
    [[150, 60], [150, 60]],                                  // head (dot)
    [[110, 70], [190, 70], [150, 95], [110, 70]],             // hat
    [[150, 95], [150, 200]],                                  // torso
    [[150, 120], [95, 160]],                                  // left arm
    [[150, 120], [205, 160]],                                 // right arm
    [[150, 200], [110, 280]],                                 // left leg
    [[150, 200], [190, 280]],                                 // right leg
  ];
  // ------------------------------------------------------------------------

  const canvas = document.querySelector('canvas');
  if (!canvas) return { error: 'no <canvas> on page — not in a drawing round?' };
  const r = canvas.getBoundingClientRect();

  const fire = (type, x, y) => {
    canvas.dispatchEvent(new PointerEvent(type, {
      clientX: r.x + x, clientY: r.y + y,
      bubbles: true, pointerId: 1, pointerType: 'pen', isPrimary: true,
      buttons: type === 'pointerup' ? 0 : 1,
      pressure: type === 'pointerup' ? 0 : 0.5,
    }));
  };

  for (const stroke of STROKES) {
    if (!stroke.length) continue;
    fire('pointerdown', stroke[0][0], stroke[0][1]);
    for (let i = 1; i < stroke.length; i++) fire('pointermove', stroke[i][0], stroke[i][1]);
    const last = stroke[stroke.length - 1];
    fire('pointerup', last[0], last[1]);
  }
  return { drew: STROKES.length, canvas: { w: r.width, h: r.height } };
}
