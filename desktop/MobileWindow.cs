using System;
using System.Collections;
using System.Collections.Generic;
using System.Drawing;
using System.IO;
using System.Windows.Forms;

namespace ZeroBridge.Desktop
{
    internal sealed class MobileWindow : Form
    {
        private readonly string root;
        private readonly bool english;
        private readonly Timer timer = new Timer { Interval = 2000 };
        private Label status;
        private ListView events;
        private PictureBox qr;
        private string lastState = "";
        private string T(string zh, string en) { return english ? en : zh; }
        public static Dictionary<string, object> ReadState(string root) { return Json.Parse(GatewayReader.ReadSmall(Path.Combine(root, "state", "mobile", "status.json"))); }
        private static Dictionary<string, object> Latest(Dictionary<string, object> state)
        {
            object items; Dictionary<string, object> last = null;
            if (state.TryGetValue("events", out items) && items is IEnumerable) foreach (var item in (IEnumerable)items) last = item as Dictionary<string, object>;
            return last;
        }
        private static bool Fresh(Dictionary<string, object> state) { double age = (DateTime.UtcNow - Json.Time(Json.Text(state, "updated"))).TotalSeconds; return age >= -5 && age < 20; }
        private static string Outcome(string outcome, bool english)
        { return outcome == "success" ? (english ? "Confirmed" : "Clash 已确认") : outcome == "pending" ? (english ? "Applying" : "执行中") : (english ? "Unconfirmed" : "结果待确认"); }
        public static string Summary(string root, bool english)
        {
            try
            {
                var state = ReadState(root); var last = Latest(state);
                bool online = string.Equals(Json.Text(state, "online"), "True", StringComparison.OrdinalIgnoreCase);
                string prefix = !Fresh(state) ? (english ? "Mobile control · offline" : "手机控制 · 状态已过期") : !online ? (english ? "Mobile control · Clash unavailable" : "手机控制 · Clash 暂不可用") : (english ? "Mobile control · online" : "手机控制 · 在线");
                if (last == null) return prefix + (english ? " · waiting for a command" : " · 等待手机指令");
                return prefix + "\n" + Json.Time(Json.Text(last, "time")).ToLocalTime().ToString("HH:mm:ss") + "  " + Json.Text(last, "group") + " → " + Json.Text(last, "target") + " · " + Outcome(Json.Text(last, "status"), english);
            }
            catch { return english ? "Mobile control · not installed or unavailable" : "手机控制 · 未安装或暂不可用"; }
        }
        public MobileWindow(string root, bool english)
        {
            SuspendLayout(); this.root = root; this.english = english;
            Text = "ZeroBridge · " + T("手机控制", "Mobile control"); Font = new Font("Microsoft YaHei UI", 9); BackColor = Style.Canvas; Size = new Size(900, 710); MinimumSize = new Size(780, 620); StartPosition = FormStartPosition.CenterScreen;
            var layout = new TableLayoutPanel { Dock = DockStyle.Fill, Padding = new Padding(24), ColumnCount = 1, RowCount = 5 };
            layout.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
            foreach (float size in new float[] { 44, 220, 66, 38 }) layout.RowStyles.Add(new RowStyle(SizeType.Absolute, size)); layout.RowStyles.Add(new RowStyle(SizeType.Percent, 100));
            layout.Controls.Add(Style.Label(T("手机选择 · 电脑同步", "Choose on phone · see it on desktop"), 19, Style.Ink, FontStyle.Bold), 0, 0);
            var pairing = new TableLayoutPanel { Dock = DockStyle.Fill, BackColor = Color.White, ColumnCount = 2, Padding = new Padding(12) }; pairing.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 192)); pairing.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
            qr = new PictureBox { Dock = DockStyle.Fill, SizeMode = PictureBoxSizeMode.Zoom };
            try { using (var image = Image.FromFile(Path.Combine(root, "state", "mobile", "pairing.png"))) qr.Image = new Bitmap(image); } catch (IOException) { } catch (ArgumentException) { }
            pairing.Controls.Add(qr, 0, 0);
            var info = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 1, RowCount = 4, Padding = new Padding(14, 0, 0, 0) }; info.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
            info.RowStyles.Add(new RowStyle(SizeType.Absolute, 65)); info.RowStyles.Add(new RowStyle(SizeType.Absolute, 33)); info.RowStyles.Add(new RowStyle(SizeType.Absolute, 33)); info.RowStyles.Add(new RowStyle(SizeType.Percent, 100));
            info.Controls.Add(Style.Label(T("iPhone 连接 ZeroTier 后扫码，使用 Safari 打开。\n配对后：分享 → 添加到主屏幕。二维码含访问密钥，请勿公开。", "Connect iPhone to ZeroTier, scan and open in Safari.\nPair, then Share → Add to Home Screen. Keep this QR private."), 9, Style.Muted, FontStyle.Regular), 0, 0);
            string address = "", token = "";
            try { var config = Json.Parse(GatewayReader.ReadSmall(Path.Combine(root, "mobile.json"))); address = "http://" + Json.Text(config, "listen") + "/"; token = Json.Text(config, "token"); } catch (Exception) { }
            var addressBox = new TextBox { Text = address, ReadOnly = true, Dock = DockStyle.Fill, BorderStyle = BorderStyle.FixedSingle }; info.Controls.Add(addressBox, 0, 1);
            var tokenBox = new TextBox { Text = token, ReadOnly = true, UseSystemPasswordChar = true, Dock = DockStyle.Fill }; info.Controls.Add(tokenBox, 0, 2);
            var buttons = new FlowLayoutPanel { Dock = DockStyle.Fill, WrapContents = false, Padding = new Padding(0, 6, 0, 0) };
            var copy = Style.Button(T("复制配对链接", "Copy pairing link"), true); copy.Enabled = token.Length > 0; copy.Width = 155; copy.Click += delegate { Clipboard.SetText(address + "#token=" + Uri.EscapeDataString(token)); };
            var reveal = new CheckBox { Text = T("显示密钥", "Show key"), AutoSize = true, Margin = new Padding(4, 11, 0, 0) }; reveal.CheckedChanged += delegate { tokenBox.UseSystemPasswordChar = !reveal.Checked; }; buttons.Controls.Add(copy); buttons.Controls.Add(reveal); info.Controls.Add(buttons, 0, 3);
            pairing.Controls.Add(info, 1, 0); layout.Controls.Add(pairing, 0, 1);
            status = Style.Label("", 10, Style.Teal, FontStyle.Regular); layout.Controls.Add(status, 0, 2);
            layout.Controls.Add(Style.Label(T("最近 20 条指令 · 每 2 秒同步 · 成功表示 Clash 当前选择已回读确认", "Last 20 commands · refreshes every 2s · confirmed by reading Clash selection"), 9, Style.Muted, FontStyle.Regular), 0, 3);
            events = new ListView { Dock = DockStyle.Fill, View = View.Details, FullRowSelect = true, GridLines = false, BorderStyle = BorderStyle.None };
            events.Columns.Add(T("时间", "Time"), 100); events.Columns.Add(T("来源", "Source"), 125); events.Columns.Add(T("代理组 → 目标", "Group → target"), 290); events.Columns.Add(T("执行结果", "Result"), 160); layout.Controls.Add(events, 0, 4); Controls.Add(layout);
            AutoScaleDimensions = new SizeF(96, 96); AutoScaleMode = AutoScaleMode.Dpi; ResumeLayout(true);
            timer.Tick += delegate { RefreshCommands(); }; timer.Start(); RefreshCommands();
        }
        private void RefreshCommands()
        {
            status.Text = Summary(root, english);
            try
            {
                string raw = GatewayReader.ReadSmall(Path.Combine(root, "state", "mobile", "status.json")); var state = Json.Parse(raw);
                string mode = Json.Text(state, "mode"); if (mode.Length > 0) status.Text += " · " + mode;
                if (raw == lastState) return; lastState = raw;
                object items; var rows = new List<ListViewItem>();
                if (state.TryGetValue("events", out items) && items is IEnumerable) foreach (var value in (IEnumerable)items)
                {
                    var item = value as Dictionary<string, object>; if (item == null) continue;
                    rows.Add(new ListViewItem(new string[] { Json.Time(Json.Text(item, "time")).ToLocalTime().ToString("HH:mm:ss"), Json.Text(item, "client"), Json.Text(item, "group") + " → " + Json.Text(item, "target"), Outcome(Json.Text(item, "status"), english) }));
                }
                rows.Reverse(); events.BeginUpdate(); events.Items.Clear(); events.Items.AddRange(rows.ToArray()); events.EndUpdate();
            }
            catch (Exception) { }
        }
        protected override void Dispose(bool disposing) { if (disposing) { timer.Dispose(); if (qr.Image != null) qr.Image.Dispose(); } base.Dispose(disposing); }
    }
}
