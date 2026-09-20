using System;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Windows.Forms;

namespace ZeroBridge.Desktop
{
    internal sealed class Dashboard : Form
    {
        private readonly GatewayReader reader;
        private readonly RateHistory history = new RateHistory();
        private readonly NotifyIcon tray = new NotifyIcon();
        private readonly Timer timer = new Timer { Interval = 2000 };
        private readonly bool minimized, readOnly;
        private bool exiting, english, busy;
        private Snapshot current = new Snapshot();
        private Process operation;
        private string action;
        private DateTime requested;
        private Icon statusIcon;
        private string iconState = "";
        private readonly string settingsPath;
        private Label headline, subtitle, status, details, footnote, chartTitle, legend, navTitle, navDetail, mobileSummary;
        private readonly Label[] values = new Label[4], captions = new Label[4];
        private Button start, stop, logs, language, phone;
        private MobileWindow mobileWindow;
        private ComboBox chartMode;
        private TrafficChart chart;
        private ToolStripMenuItem trayShow, trayStart, trayStop, trayLogs, trayExit;

        public Dashboard(string root, bool minimized, bool readOnly)
        {
            SuspendLayout();
            reader = new GatewayReader(root); this.minimized = minimized; this.readOnly = readOnly;
            settingsPath = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "ZeroBridgeMihomo", "language.txt");
            try { english = File.Exists(settingsPath) && File.ReadAllText(settingsPath).Trim() == "en"; } catch (IOException) { }
            Text = "ZeroBridge Mihomo"; Font = new Font("Microsoft YaHei UI", 9);
            StartPosition = FormStartPosition.CenterScreen; Size = new Size(1100, 800); MinimumSize = new Size(1000, 750);
            BackColor = Style.Canvas;
            BuildWindow(); BuildTray(); Translate(); RefreshState();
            AutoScaleDimensions = new SizeF(96, 96); AutoScaleMode = AutoScaleMode.Dpi; ResumeLayout(true);
            timer.Tick += delegate { FinishOperation(); RefreshState(); }; timer.Start();
            Shown += delegate { if (minimized) Hide(); };
            Resize += delegate { if (WindowState == FormWindowState.Minimized) Hide(); };
            FormClosing += delegate(object sender, FormClosingEventArgs e) { if (!exiting && e.CloseReason == CloseReason.UserClosing) { e.Cancel = true; Hide(); } };
        }
        private string T(string zh, string en) { return english ? en : zh; }
        private void BuildWindow()
        {
            var sidebar = new Panel { Dock = DockStyle.Left, Width = 202, BackColor = Style.Navy, Padding = new Padding(24, 32, 20, 24) };
            var brand = Style.Label("ZeroBridge", 17, Color.White, FontStyle.Bold); brand.Dock = DockStyle.Top; brand.Height = 40;
            var second = Style.Label("MIHOMO  /  WINDOWS", 8, Color.FromArgb(139, 161, 181), FontStyle.Regular); second.Dock = DockStyle.Top; second.Height = 30;
            navTitle = Style.Label("", 12, Color.FromArgb(96, 229, 212), FontStyle.Bold); navTitle.Dock = DockStyle.Top; navTitle.Height = 84;
            navDetail = Style.Label("", 9, Color.FromArgb(152, 169, 187), FontStyle.Regular); navDetail.Dock = DockStyle.Bottom; navDetail.Height = 108;
            var version = Style.Label("DESKTOP  v" + Program.Version, 9, Color.FromArgb(152, 169, 187), FontStyle.Regular); version.Dock = DockStyle.Bottom; version.Height = 25;
            sidebar.Controls.Add(navTitle); sidebar.Controls.Add(second); sidebar.Controls.Add(brand); sidebar.Controls.Add(navDetail); sidebar.Controls.Add(version);
            var main = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 1, RowCount = 9, Padding = new Padding(28, 22, 28, 20) };
            main.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
            foreach (float height in new float[] { 48, 30, 48, 100, 48 }) main.RowStyles.Add(new RowStyle(SizeType.Absolute, height));
            main.RowStyles.Add(new RowStyle(SizeType.Percent, 100)); main.RowStyles.Add(new RowStyle(SizeType.Absolute, 74)); main.RowStyles.Add(new RowStyle(SizeType.Absolute, 56)); main.RowStyles.Add(new RowStyle(SizeType.Absolute, 48));
            var top = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 2, Margin = new Padding(0) };
            top.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100)); top.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 108));
            headline = Style.Label("", 23, Style.Ink, FontStyle.Bold); language = Style.Button("English", false); language.Width = 104; language.Height = 34; language.Anchor = AnchorStyles.Right;
            language.Click += delegate { english = !english; try { Directory.CreateDirectory(Path.GetDirectoryName(settingsPath)); File.WriteAllText(settingsPath, english ? "en" : "zh"); } catch (IOException) { } Translate(); RefreshState(); };
            top.Controls.Add(headline, 0, 0); top.Controls.Add(language, 1, 0); main.Controls.Add(top, 0, 0);
            subtitle = Style.Label("", 9, Style.Muted, FontStyle.Regular); main.Controls.Add(subtitle, 0, 1);
            status = Style.Label("", 11, Style.Teal, FontStyle.Bold); main.Controls.Add(status, 0, 2);
            var cards = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 4, Margin = new Padding(0) };
            for (int i = 0; i < 4; i++)
            {
                cards.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 25));
                var card = new Panel { Dock = DockStyle.Fill, BackColor = Color.White, Padding = new Padding(16, 10, 10, 10), Margin = new Padding(0, 0, i == 3 ? 0 : 12, 0) };
                captions[i] = Style.Label("", 9, Style.Muted, FontStyle.Regular); captions[i].Dock = DockStyle.Bottom; captions[i].Height = 27;
                values[i] = Style.Label("—", 24, i == 1 ? Style.Teal : i == 2 ? Style.Blue : Style.Ink, FontStyle.Bold);
                if (i == 3) { values[i].Font.Dispose(); values[i].Font = new Font("Microsoft YaHei UI", 18, FontStyle.Bold); }
                card.Controls.Add(values[i]); card.Controls.Add(captions[i]); cards.Controls.Add(card, i, 0);
            }
            main.Controls.Add(cards, 0, 3);
            var chartHeader = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 2, Margin = new Padding(0, 8, 0, 0) };
            chartHeader.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100)); chartHeader.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 190));
            chartTitle = Style.Label("", 11, Style.Ink, FontStyle.Bold);
            chartMode = new ComboBox { DropDownStyle = ComboBoxStyle.DropDownList, Width = 185, Anchor = AnchorStyles.Right, AccessibleName = "Chart metric" };
            chartMode.SelectedIndexChanged += delegate { chart.Connections = chartMode.SelectedIndex == 1; UpdateLegend(); chart.Invalidate(); };
            chartHeader.Controls.Add(chartTitle, 0, 0); chartHeader.Controls.Add(chartMode, 1, 0); main.Controls.Add(chartHeader, 0, 4);
            var chartPanel = new Panel { Dock = DockStyle.Fill, BackColor = Color.White, Padding = new Padding(8), Margin = new Padding(0) };
            chart = new TrafficChart { Dock = DockStyle.Fill, History = history };
            legend = Style.Label("", 8, Style.Muted, FontStyle.Regular); legend.Dock = DockStyle.Bottom; legend.Height = 25;
            chartPanel.Controls.Add(chart); chartPanel.Controls.Add(legend); main.Controls.Add(chartPanel, 0, 5);
            details = Style.Label("", 9, Style.Muted, FontStyle.Regular); details.AutoEllipsis = false; details.TextAlign = ContentAlignment.TopLeft; details.Padding = new Padding(0, 10, 0, 0); main.Controls.Add(details, 0, 6);
            var buttons = new FlowLayoutPanel { Dock = DockStyle.Fill, Margin = new Padding(0), WrapContents = false };
            mobileSummary = Style.Label("", 9, Style.Teal, FontStyle.Regular); mobileSummary.BackColor = Color.White; mobileSummary.Padding = new Padding(12, 0, 12, 0); mobileSummary.Margin = new Padding(0, 0, 0, 10); main.Controls.Add(mobileSummary, 0, 7);
            start = Style.Button("", true); stop = Style.Button("", false); logs = Style.Button("", false); phone = Style.Button("", false); phone.Click += delegate { OpenMobile(); };
            start.Click += delegate { RequestOperation("Start"); }; stop.Click += delegate { RequestOperation("Stop"); }; logs.Click += delegate { OpenLogs(); };
            buttons.Controls.AddRange(new Control[] { start, stop, logs, phone }); main.Controls.Add(buttons, 0, 8);
            footnote = Style.Label("", 8, Style.Muted, FontStyle.Regular); footnote.Dock = DockStyle.Bottom; footnote.Height = 32; footnote.Padding = new Padding(230, 0, 20, 0);
            Controls.Add(main); Controls.Add(sidebar); Controls.Add(footnote);
        }
        private void BuildTray()
        {
            var menu = new ContextMenuStrip();
            trayShow = new ToolStripMenuItem(); trayShow.Click += delegate { ShowDashboard(); };
            trayStart = new ToolStripMenuItem(); trayStart.Click += delegate { RequestOperation("Start"); };
            trayStop = new ToolStripMenuItem(); trayStop.Click += delegate { RequestOperation("Stop"); };
            trayLogs = new ToolStripMenuItem(); trayLogs.Click += delegate { OpenLogs(); };
            var project = new ToolStripMenuItem("GitHub / ZeroBridge Mihomo"); project.Click += delegate { OpenPath("https://github.com/Luase233/zerobridge-mihomo"); };
            trayExit = new ToolStripMenuItem(); trayExit.Click += delegate { exiting = true; Close(); };
            var mobile = new ToolStripMenuItem("手机控制 / Mobile control"); mobile.Click += delegate { OpenMobile(); };
            menu.Items.AddRange(new ToolStripItem[] { trayShow, mobile, new ToolStripSeparator(), trayStart, trayStop, trayLogs, project, new ToolStripSeparator(), trayExit });
            tray.ContextMenuStrip = menu; tray.Text = "ZeroBridge Mihomo"; tray.DoubleClick += delegate { ShowDashboard(); }; tray.Visible = true;
        }
        public void ShowDashboard() { Show(); WindowState = FormWindowState.Normal; Activate(); }
        private void Translate()
        {
            headline.Text = T("网关概览", "Gateway overview"); language.Text = english ? "简体中文" : "English";
            subtitle.Text = T("ZeroTier → 本机转发 → Mihomo · 实时运行状态", "ZeroTier → local forwarding → Mihomo · live status");
            navTitle.Text = T("●  运行面板", "●  Dashboard"); navDetail.Text = T("本地转发工具\n不提供 VPN 服务\n关闭窗口将收起到托盘", "Local forwarding tool\nNo VPN service provided\nClose window to hide to tray");
            captions[0].Text = T("活动连接", "Active connections"); captions[1].Text = T("接收 · 包/秒", "Received · packets/s"); captions[2].Text = T("发送 · 包/秒", "Sent · packets/s"); captions[3].Text = T("拒绝 / 错误 · 累计", "Rejected / errors · total");
            chartTitle.Text = T("最近 10 分钟", "Last 10 minutes");
            int selected = Math.Max(0, chartMode.SelectedIndex); chartMode.Items.Clear(); chartMode.Items.Add(T("收发包速率", "Packet rate")); chartMode.Items.Add(T("活动连接", "Active connections")); chartMode.SelectedIndex = selected;
            chart.English = english; UpdateLegend(); chart.Invalidate();
            start.Text = trayStart.Text = T("启动网关", "Start gateway"); stop.Text = trayStop.Text = T("停止网关", "Stop gateway"); logs.Text = trayLogs.Text = T("打开日志", "Open logs");
            phone.Text = T("手机控制", "Mobile control");
            trayShow.Text = T("打开运行面板", "Open dashboard"); trayExit.Text = T("退出界面（网关继续运行）", "Exit UI (keep gateway running)");
            footnote.Text = T("本项目仅提供本地转发与协议转换，不提供 VPN、节点或订阅服务。", "Local forwarding and protocol conversion only. No VPN, proxy nodes or subscriptions provided.");
        }
        private void UpdateLegend() { legend.Text = chart.Connections ? T("  绿色：活动连接数", "  Green: active connections") : T("  绿色：接收    蓝色：发送    · 包速率，非带宽；拒绝计数不等于丢包率", "  Green: received    Blue: sent    · packets/s, not bandwidth; rejected is not packet loss"); }
        private void RefreshState()
        {
            current = reader.Read(DateTime.UtcNow); history.Update(current);
            string state = current.Ready ? "ready" : current.State == "stopped" ? "stopped" : "warning";
            string label = current.Ready ? T("运行中 · 心跳正常", "Running · heartbeat healthy") : current.State == "stopped" ? T("已停止", "Stopped") : T("待检查 · 状态未就绪或已过期", "Attention · not ready or stale");
            if (busy) label = T("正在执行管理操作…", "Management operation in progress…");
            status.Text = "●  " + label + (readOnly ? T(" · 只读", " · read only") : ""); status.ForeColor = state == "ready" ? Style.Teal : state == "warning" ? Style.Warning : Style.Muted;
            if (iconState != state)
            {
                Icon old = statusIcon; statusIcon = Style.MakeIcon(status.ForeColor); tray.Icon = statusIcon; Icon = statusIcon; if (old != null) old.Dispose(); iconState = state;
            }
            tray.Text = "ZeroBridge Mihomo · " + (state == "ready" ? "Running" : state == "stopped" ? "Stopped" : "Attention");
            bool sample = current.Ready && current.HasStats;
            values[0].Text = sample ? current.Active.ToString("0") : "—";
            values[1].Text = sample && history.Points.Count > 1 ? history.Current.InRate.ToString("0.#") : "—";
            values[2].Text = sample && history.Points.Count > 1 ? history.Current.OutRate.ToString("0.#") : "—";
            values[3].Text = sample ? current.Rejected.ToString("0") + " / " + current.Errors.ToString("0") : "—";
            string sampleText = sample ? current.SampleTime.ToLocalTime().ToString("HH:mm:ss") : T("无新鲜样本", "No fresh sample");
            details.Text = T("客户端 ", "Client ") + current.Source + "    →    SOCKS " + current.Upstream + "\n" + T("最近采样 ", "Last sample ") + sampleText + "   ·   " + current.State + (current.Error.Length > 0 ? " · " + current.Error : "") + "\n" + reader.Root;
            start.Enabled = trayStart.Enabled = !readOnly && !busy && current.State == "stopped";
            stop.Enabled = trayStop.Enabled = !readOnly && !busy && current.State != "stopped";
            chart.Invalidate();
            mobileSummary.Text = MobileWindow.Summary(reader.Root, english);
        }
        private void OpenMobile() { if (mobileWindow == null || mobileWindow.IsDisposed) mobileWindow = new MobileWindow(reader.Root, english); mobileWindow.Show(); mobileWindow.Activate(); }
        private void RequestOperation(string requestedAction)
        {
            if (readOnly || busy) return;
            ShowDashboard();
            using (var dialog = new Form { Text = T("操作前确认", "Before continuing"), ClientSize = new Size(520, 228), FormBorderStyle = FormBorderStyle.FixedDialog, MaximizeBox = false, MinimizeBox = false, StartPosition = FormStartPosition.CenterParent, Font = Font })
            {
                var text = new Label { Text = T("启动或停止会切换网关及 NAT 状态。\n请先在手机 ZeroTier 中关闭 Enable Default Route，\n并保持关闭，直到操作完成并确认网关状态。", "Starting or stopping changes gateway and NAT state.\nTurn OFF Enable Default Route in the phone's ZeroTier app.\nKeep it off until the operation and status checks complete."), Location = new Point(24, 22), Size = new Size(475, 83) };
                var check = new CheckBox { Text = T("我已关闭手机的 Enable Default Route", "I have turned OFF Enable Default Route on the phone"), Location = new Point(24, 114), Size = new Size(475, 30) };
                var proceed = Style.Button(T("继续（管理员权限）", "Continue (administrator)"), true); proceed.Width = 240; proceed.Location = new Point(24, 169); proceed.Enabled = false; proceed.DialogResult = DialogResult.OK;
                var cancel = Style.Button(T("取消", "Cancel"), false); cancel.Location = new Point(346, 169); cancel.DialogResult = DialogResult.Cancel;
                check.CheckedChanged += delegate { proceed.Enabled = check.Checked; }; dialog.Controls.AddRange(new Control[] { text, check, proceed, cancel }); dialog.CancelButton = cancel;
                if (dialog.ShowDialog(this) != DialogResult.OK || !check.Checked) return;
            }
            try
            {
                string script = Path.Combine(reader.Root, "scripts", "Manage.ps1");
                if (!File.Exists(script)) throw new FileNotFoundException("Manage.ps1 was not found.", script);
                action = requestedAction; requested = DateTime.UtcNow;
                operation = Process.Start(new ProcessStartInfo {
                    FileName = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.Windows), @"System32\WindowsPowerShell\v1.0\powershell.exe"),
                    Arguments = "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File \"" + script + "\" -Action " + action + " -PhoneDefaultRouteIsOff",
                    UseShellExecute = true, Verb = "runas", WindowStyle = ProcessWindowStyle.Hidden, WorkingDirectory = reader.Root });
                busy = true; RefreshState();
            }
            catch (Exception ex) { MessageBox.Show(this, ex.Message, "ZeroBridge Mihomo", MessageBoxButtons.OK, MessageBoxIcon.Warning); }
        }
        private void FinishOperation()
        {
            if (operation == null || !operation.HasExited) return;
            bool success = false; string message = T("操作未确认完成，请检查日志。", "Completion was not confirmed. Please inspect the logs.");
            try
            {
                var result = Json.Parse(GatewayReader.ReadSmall(Path.Combine(reader.Root, "state", "manage-result.json")));
                if (Json.Text(result, "Action") == action && Json.Time(Json.Text(result, "UpdatedUtc")) >= requested.AddSeconds(-1))
                {
                    success = operation.ExitCode == 0 && Json.Text(result, "Status") == "completed";
                    message = success ? T("管理操作已完成。请确认网关状态，再按需开启手机 Default Route。", "Management operation completed. Check gateway status before enabling Default Route.") : Json.Text(result, "Error");
                }
            }
            catch (Exception ex) { message += "\n" + ex.Message; }
            operation.Dispose(); operation = null; busy = false; RefreshState(); ShowDashboard();
            MessageBox.Show(this, message, "ZeroBridge Mihomo", MessageBoxButtons.OK, success ? MessageBoxIcon.Information : MessageBoxIcon.Warning);
        }
        private void OpenLogs() { string path = Path.Combine(reader.Root, "state"); if (Directory.Exists(path)) OpenPath(path); }
        private void OpenPath(string path) { try { Process.Start(new ProcessStartInfo(path) { UseShellExecute = true }); } catch (Exception ex) { MessageBox.Show(this, ex.Message); } }
        protected override void Dispose(bool disposing)
        {
            if (disposing) { timer.Dispose(); tray.Visible = false; tray.Dispose(); if (mobileWindow != null) mobileWindow.Dispose(); if (statusIcon != null) statusIcon.Dispose(); if (operation != null) operation.Dispose(); }
            base.Dispose(disposing);
        }
    }
}
