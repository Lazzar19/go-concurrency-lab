"use strict";
function draw(canvas, series, options) {
    const opts = options ?? {};
    const width = canvas.clientWidth || 320;
    const height = canvas.clientHeight || 220;
    const dpr = window.devicePixelRatio || 1;
    canvas.width = Math.round(width * dpr);
    canvas.height = Math.round(height * dpr);
    const ctx = canvas.getContext("2d");
    if (!ctx)
        return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height);
    const pad = { left: 58, right: 12, top: 16, bottom: 28 };
    const plotW = Math.max(1, width - pad.left - pad.right);
    const plotH = Math.max(1, height - pad.top - pad.bottom);
    ctx.fillStyle = "#fbfbfc";
    ctx.fillRect(0, 0, width, height);
    const points = [];
    for (const line of series) {
        for (const point of line.data) {
            if (Number.isFinite(point.x) && Number.isFinite(point.y))
                points.push(point);
        }
    }
    if (!points.length) {
        ctx.fillStyle = "#5c6573";
        ctx.font = "12px ui-monospace, monospace";
        ctx.fillText(opts.empty ?? "Waiting for a run", pad.left, pad.top + 18);
        return;
    }
    let minX = points[0].x;
    let maxX = points[0].x;
    let maxY = 0;
    for (const point of points) {
        minX = Math.min(minX, point.x);
        maxX = Math.max(maxX, point.x);
        maxY = Math.max(maxY, point.y);
    }
    if (maxX <= minX)
        maxX = minX + 1;
    if (maxY <= 0)
        maxY = 1;
    ctx.strokeStyle = "#d5dae2";
    ctx.fillStyle = "#5c6573";
    ctx.lineWidth = 1;
    ctx.font = "11px ui-monospace, monospace";
    for (let tick = 0; tick <= 4; tick++) {
        const y = pad.top + (plotH * tick) / 4;
        const value = maxY * (1 - tick / 4);
        ctx.beginPath();
        ctx.moveTo(pad.left, y);
        ctx.lineTo(pad.left + plotW, y);
        ctx.stroke();
        ctx.fillText(formatTick(value), 6, y + 4);
    }
    ctx.fillText(opts.xLabel ?? "ms", pad.left, height - 8);
    for (const line of series) {
        if (!line.data.length)
            continue;
        ctx.beginPath();
        ctx.strokeStyle = line.color || "#14161c";
        ctx.lineWidth = 1.6;
        line.data.forEach((point, index) => {
            const x = pad.left + ((point.x - minX) / (maxX - minX)) * plotW;
            const y = pad.top + (1 - point.y / maxY) * plotH;
            if (index === 0)
                ctx.moveTo(x, y);
            else
                ctx.lineTo(x, y);
        });
        ctx.stroke();
    }
}
function formatTick(value) {
    const abs = Math.abs(value);
    if (abs >= 1000000)
        return (value / 1000000).toFixed(1) + "M";
    if (abs >= 1000)
        return (value / 1000).toFixed(1) + "k";
    if (abs >= 10)
        return value.toFixed(0);
    if (abs >= 1)
        return value.toFixed(1);
    if (abs >= 0.01)
        return value.toFixed(2);
    return value.toFixed(3);
}
window.LabCharts = { draw };
