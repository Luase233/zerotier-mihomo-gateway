using System;
using System.IO;
using System.Security.Cryptography;
using System.Text;
using System.Threading;
using System.Windows.Forms;

namespace ZeroBridge.Desktop
{
    internal static class Program
    {
        public const string Version = "0.3.0";
        [STAThread]
        private static void Main(string[] args)
        {
            Application.EnableVisualStyles();
            Application.SetCompatibleTextRenderingDefault(false);
            try
            {
                string root = @"C:\ZeroTierGateway"; bool minimized = false, readOnly = false;
                for (int i = 0; i < args.Length; i++)
                {
                    if (args[i] == "--root" && i + 1 < args.Length) root = args[++i];
                    else if (args[i] == "--minimized") minimized = true;
                    else if (args[i] == "--readonly") readOnly = true;
                    else throw new ArgumentException("Usage: zerobridge-desktop.exe [--root path] [--minimized] [--readonly]");
                }
                root = Path.GetFullPath(root).TrimEnd(Path.DirectorySeparatorChar);
                // The root is passed as one PowerShell -File argument, never as executable script text.
                if (root.IndexOf('"') >= 0 || root.IndexOf('\r') >= 0 || root.IndexOf('\n') >= 0) throw new ArgumentException("Invalid root path.");
                string key;
                using (var sha = SHA256.Create()) key = BitConverter.ToString(sha.ComputeHash(Encoding.UTF8.GetBytes(root.ToUpperInvariant()))).Replace("-", "").Substring(0, 24);
                bool created;
                using (var mutex = new Mutex(true, @"Local\ZeroBridgeMihomo-" + key, out created))
                using (var signal = new EventWaitHandle(false, EventResetMode.AutoReset, @"Local\ZeroBridgeMihomo-Show-" + key))
                {
                    if (!created) { signal.Set(); return; }
                    using (var form = new Dashboard(root, minimized, readOnly))
                    using (var timer = new System.Windows.Forms.Timer { Interval = 300 })
                    {
                        timer.Tick += delegate { if (signal.WaitOne(0)) form.ShowDashboard(); };
                        timer.Start(); Application.Run(form);
                    }
                    mutex.ReleaseMutex();
                }
            }
            catch (Exception ex) { MessageBox.Show(ex.Message, "ZeroBridge Mihomo", MessageBoxButtons.OK, MessageBoxIcon.Error); }
        }
    }
}
