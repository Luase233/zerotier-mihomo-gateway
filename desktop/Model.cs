using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using System.Text;
using System.Text.RegularExpressions;
using System.Web.Script.Serialization;

namespace ZeroBridge.Desktop
{
    internal static class Json
    {
        public static Dictionary<string, object> Parse(string value)
        {
            return new JavaScriptSerializer { MaxJsonLength = 131072, RecursionLimit = 32 }
                .Deserialize<Dictionary<string, object>>(value);
        }
        public static string Text(Dictionary<string, object> obj, string key)
        {
            object value;
            return obj != null && obj.TryGetValue(key, out value) && value != null ? Convert.ToString(value, CultureInfo.InvariantCulture) : "";
        }
        public static Dictionary<string, object> Object(Dictionary<string, object> obj, string key)
        {
            object value;
            return obj != null && obj.TryGetValue(key, out value) ? value as Dictionary<string, object> : null;
        }
        public static double Number(Dictionary<string, object> obj, string key)
        {
            double value;
            return double.TryParse(Text(obj, key), NumberStyles.Float, CultureInfo.InvariantCulture, out value)
                && !double.IsNaN(value) && !double.IsInfinity(value) && value >= 0 ? value : 0;
        }
        public static DateTime Time(string value)
        {
            DateTimeOffset parsed;
            value = Regex.Replace(value ?? "", @"(\.\d{7})\d+(?=Z|[+-])", "$1");
            return DateTimeOffset.TryParse(value, CultureInfo.InvariantCulture, DateTimeStyles.None, out parsed)
                ? parsed.UtcDateTime : DateTime.MinValue;
        }
    }

    internal sealed class Snapshot
    {
        public string State = "stopped", Error = "", Source = "", Upstream = "", LogPath = "", Identity = "";
        public DateTime Heartbeat, SampleTime, Started;
        public double In, Out, Active, TCP, UDP, Rejected, Errors;
        public bool HasStats, Fresh;
        public bool Ready { get { return State == "ready" && Fresh; } }
    }

    internal sealed class GatewayReader
    {
        public readonly string Root;
        public GatewayReader(string root) { Root = Path.GetFullPath(root).TrimEnd(Path.DirectorySeparatorChar); }
        public static bool Within(string path, string root)
        {
            return Path.GetFullPath(path).StartsWith(Path.GetFullPath(root).TrimEnd(Path.DirectorySeparatorChar) + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase);
        }
        public static string ReadSmall(string path)
        {
            using (var file = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite | FileShare.Delete))
            {
                if (file.Length > 131072) throw new IOException("State file exceeds the size limit.");
                using (var reader = new StreamReader(file, Encoding.UTF8, true)) return reader.ReadToEnd();
            }
        }
        public static string Tail(string path, int limit)
        {
            using (var file = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite | FileShare.Delete))
            {
                long start = Math.Max(0, file.Length - limit);
                file.Seek(start, SeekOrigin.Begin);
                using (var reader = new StreamReader(file, Encoding.UTF8, true))
                {
                    string text = reader.ReadToEnd();
                    if (start > 0) { int line = text.IndexOf('\n'); text = line < 0 ? "" : text.Substring(line + 1); }
                    return text;
                }
            }
        }
        public Snapshot Read(DateTime now)
        {
            var s = new Snapshot();
            try
            {
                string configPath = Path.Combine(Root, "gateway.json");
                if (File.Exists(configPath))
                {
                    var config = Json.Parse(ReadSmall(configPath));
                    s.Source = Json.Text(config, "source_ip"); s.Upstream = Json.Text(config, "proxy");
                }
                string path = Path.Combine(Root, "state", "runtime.json");
                if (!File.Exists(path)) return s;
                var state = Json.Parse(ReadSmall(path));
                if (Json.Text(state, "Task") != "zt-socks-gateway") throw new IOException("Unexpected gateway state format.");
                s.Heartbeat = File.GetLastWriteTimeUtc(path);
                s.Fresh = (now - s.Heartbeat).TotalSeconds >= -5 && (now - s.Heartbeat).TotalSeconds < 20;
                s.State = Json.Text(state, "Status"); s.Error = Json.Text(state, "LastError");
                s.Started = Json.Time(Json.Text(state, "StartedUtc"));
                s.Identity = Json.Text(state, "RunId") + ":" + Json.Text(Json.Object(state, "Proxy"), "Pid");
                string directory = Json.Text(state, "ProxyDirectory");
                if (string.IsNullOrEmpty(directory)) directory = Json.Text(state, "RunDirectory");
                if (string.IsNullOrEmpty(directory)) return s;
                if (!Within(directory, Path.Combine(Root, "state", "runs"))) throw new IOException("Log path is outside this gateway's run directory.");
                s.LogPath = Path.Combine(directory, "proxy.stderr.log");
                if (!File.Exists(s.LogPath)) return s;
                string[] lines = Tail(s.LogPath, 65536).Split('\n');
                for (int i = lines.Length - 1; i >= 0; i--)
                {
                    try
                    {
                        var record = Json.Parse(lines[i].Trim());
                        if (Json.Text(record, "msg") != "stats") continue;
                        var stats = Json.Object(record, "gateway"); if (stats == null) continue;
                        s.SampleTime = Json.Time(Json.Text(record, "time"));
                        s.In = Json.Number(stats, "packets_in"); s.Out = Json.Number(stats, "packets_out");
                        s.Active = Json.Number(stats, "active"); s.TCP = Json.Number(stats, "tcp_flows"); s.UDP = Json.Number(stats, "udp_flows");
                        s.Rejected = Json.Number(stats, "rejected"); s.Errors = Json.Number(stats, "errors");
                        s.HasStats = s.SampleTime != DateTime.MinValue && (now - s.SampleTime).TotalSeconds >= -5 && (now - s.SampleTime).TotalSeconds < 20;
                        break;
                    }
                    catch (ArgumentException) { }
                    catch (InvalidOperationException) { }
                }
                return s;
            }
            catch (Exception ex)
            {
                s.State = "unavailable"; s.Fresh = false; s.HasStats = false; s.Error = ex.Message; return s;
            }
        }
    }

    internal sealed class Sample
    {
        public DateTime Time;
        public double InRate, OutRate, Active;
    }

    internal sealed class RateHistory
    {
        private Snapshot previous;
        public readonly List<Sample> Points = new List<Sample>();
        public Sample Current;
        public void Update(Snapshot next)
        {
            if (!next.HasStats || !next.Ready) { previous = null; Current = null; return; }
            if (previous != null && next.Identity == previous.Identity && next.SampleTime == previous.SampleTime) return;
            var point = new Sample { Time = next.SampleTime, Active = next.Active };
            if (previous != null && previous.Identity == next.Identity)
            {
                double seconds = (next.SampleTime - previous.SampleTime).TotalSeconds;
                if (seconds > 0 && seconds <= 30 && next.In >= previous.In && next.Out >= previous.Out)
                { point.InRate = (next.In - previous.In) / seconds; point.OutRate = (next.Out - previous.Out) / seconds; }
            }
            if (previous == null || previous.Identity != next.Identity) Points.Clear();
            Points.Add(point); if (Points.Count > 120) Points.RemoveAt(0);
            previous = next; Current = point;
        }
    }
}
