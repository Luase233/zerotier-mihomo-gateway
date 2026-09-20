using System;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.Runtime.InteropServices;
using System.Windows.Forms;

namespace ZeroBridge.Desktop
{
    internal static class Style
    {
        public static readonly Color Ink = Color.FromArgb(24, 36, 54), Muted = Color.FromArgb(106, 119, 137),
            Canvas = Color.FromArgb(243, 246, 250), Navy = Color.FromArgb(19, 29, 46),
            Teal = Color.FromArgb(0, 151, 139), Blue = Color.FromArgb(72, 118, 224),
            Line = Color.FromArgb(228, 234, 241), Warning = Color.FromArgb(188, 119, 29);
        [DllImport("user32.dll")] private static extern bool DestroyIcon(IntPtr icon);
        public static Icon MakeIcon(Color state)
        {
            using (var bitmap = new Bitmap(64, 64))
            using (var g = Graphics.FromImage(bitmap))
            {
                g.SmoothingMode = SmoothingMode.AntiAlias; g.Clear(Color.Transparent);
                using (var background = new SolidBrush(Navy)) g.FillEllipse(background, 1, 1, 62, 62);
                using (var pen = new Pen(Color.FromArgb(96, 229, 212), 5))
                {
                    pen.StartCap = pen.EndCap = LineCap.Round;
                    g.DrawLine(pen, 15, 22, 15, 44); g.DrawLine(pen, 45, 22, 45, 44);
                    g.DrawBezier(pen, 15, 26, 23, 39, 37, 39, 45, 26); g.DrawLine(pen, 12, 44, 49, 44);
                }
                using (var brush = new SolidBrush(state)) g.FillEllipse(brush, 45, 43, 17, 17);
                using (var pen = new Pen(Color.White, 2)) g.DrawEllipse(pen, 45, 43, 17, 17);
                IntPtr handle = bitmap.GetHicon();
                try { using (var icon = Icon.FromHandle(handle)) return (Icon)icon.Clone(); }
                finally { DestroyIcon(handle); }
            }
        }
        public static Label Label(string text, float size, Color color, FontStyle fontStyle)
        {
            return new Label { Text = text, Font = new Font("Microsoft YaHei UI", size, fontStyle), ForeColor = color,
                AutoSize = false, AutoEllipsis = true, Dock = DockStyle.Fill, TextAlign = ContentAlignment.MiddleLeft, Margin = new Padding(0) };
        }
        public static Button Button(string text, bool primary)
        {
            var button = new Button { Text = text, Width = 144, Height = 38, FlatStyle = FlatStyle.Flat,
                BackColor = primary ? Teal : Color.White, ForeColor = primary ? Color.White : Ink,
                Cursor = Cursors.Hand, Margin = new Padding(0, 0, 10, 0), UseVisualStyleBackColor = false };
            button.FlatAppearance.BorderColor = primary ? Teal : Line;
            return button;
        }
    }

    internal sealed class TrafficChart : Control
    {
        public RateHistory History;
        public bool Connections, English;
        public TrafficChart() { DoubleBuffered = true; BackColor = Color.White; AccessibleName = "Gateway traffic chart"; }
        protected override void OnPaint(PaintEventArgs e)
        {
            base.OnPaint(e);
            var g = e.Graphics; g.SmoothingMode = SmoothingMode.AntiAlias;
            var area = new RectangleF(54, 14, Math.Max(1, Width - 78), Math.Max(1, Height - 49));
            double max = 1;
            if (History != null) foreach (var p in History.Points) max = Math.Max(max, Connections ? p.Active : Math.Max(p.InRate, p.OutRate));
            max = Math.Ceiling(max * 1.15);
            using (var pen = new Pen(Style.Line))
            using (var brush = new SolidBrush(Style.Muted))
            using (var font = new Font("Segoe UI", 8))
            {
                for (int i = 0; i <= 4; i++)
                {
                    float y = area.Top + area.Height * i / 4;
                    g.DrawLine(pen, area.Left, y, area.Right, y);
                    g.DrawString((max * (4 - i) / 4).ToString("0.#"), font, brush, 4, y - 6);
                }
                if (History == null || History.Current == null || History.Points.Count < 2)
                {
                    string message = English ? "Waiting for fresh gateway samples…" : "等待网关统计样本…";
                    using (var format = new StringFormat { Alignment = StringAlignment.Center, LineAlignment = StringAlignment.Center })
                        g.DrawString(message, Font, brush, area, format);
                    return;
                }
                var points = History.Points;
                DateTime last = points[points.Count - 1].Time;
                DateTime first = last.AddMinutes(-10);
                DrawLine(g, area, max, first, last, false, Connections ? Style.Teal : Style.Teal);
                if (!Connections) DrawLine(g, area, max, first, last, true, Style.Blue);
                g.DrawString(English ? "10 min ago" : "10 分钟前", font, brush, area.Left, area.Bottom + 12);
                g.DrawString(English ? "5 min" : "5 分钟前", font, brush, area.Left + area.Width / 2 - 12, area.Bottom + 12);
                g.DrawString(English ? "Now" : "现在", font, brush, area.Right - 25, area.Bottom + 12);
            }
        }
        private void DrawLine(Graphics g, RectangleF area, double max, DateTime first, DateTime last, bool outgoing, Color color)
        {
            PointF? previous = null;
            using (var pen = new Pen(color, 2.4f))
            {
                foreach (var p in History.Points)
                {
                    if (p.Time < first) continue;
                    double value = Connections ? p.Active : outgoing ? p.OutRate : p.InRate;
                    var current = new PointF(area.Left + (float)((p.Time - first).TotalSeconds / 600) * area.Width,
                        area.Bottom - (float)(value / max) * area.Height);
                    if (previous.HasValue) g.DrawLine(pen, previous.Value, current);
                    previous = current;
                }
            }
        }
    }
}
