using System;
using System.IO;
using ZeroBridge.Desktop;

internal static class DesktopModelTests
{
    private static int checks;
    private static void Check(bool condition, string name) { if (!condition) throw new Exception(name); checks++; }
    private static Snapshot Sample(DateTime time, string id, double input, double output) { return new Snapshot { State = "ready", Fresh = true, HasStats = true, SampleTime = time, Identity = id, In = input, Out = output, Active = 3 }; }
    public static void Main()
    {
        string root = Path.Combine(Path.GetTempPath(), "ZeroBridgeTests-" + Guid.NewGuid().ToString("N"));
        try
        {
            var now = DateTime.UtcNow; var history = new RateHistory();
            history.Update(Sample(now, "one", 10, 20)); history.Update(Sample(now.AddSeconds(5), "one", 60, 120));
            Check(history.Current.InRate == 10 && history.Current.OutRate == 20, "Rates use counter differences and elapsed seconds");
            history.Update(Sample(now.AddSeconds(5), "one", 60, 120)); Check(history.Points.Count == 2, "Duplicate samples ignored");
            history.Update(Sample(now.AddSeconds(10), "two", 1, 2)); Check(history.Points.Count == 1 && history.Current.InRate == 0, "Process restart resets rates");
            history.Update(new Snapshot()); Check(history.Current == null, "Stale data pauses chart");
            Check(!GatewayReader.Within(root + "-outside", root), "Sibling path rejected");
            Check(!GatewayReader.Within(Path.Combine(root, "runs", "..", "..", "outside"), root), "Traversal rejected");
            string runs = Path.Combine(root, "state", "runs", "test"); Directory.CreateDirectory(runs);
            string manifest = Path.Combine(root, "state", "runtime.json");
            var serializer = new System.Web.Script.Serialization.JavaScriptSerializer();
            File.WriteAllText(manifest, serializer.Serialize(new { Task = "zt-socks-gateway", Status = "ready", RunId = "one", Proxy = new { Pid = 5 }, ProxyDirectory = runs, StartedUtc = now.ToString("o") }));
            string log = Path.Combine(runs, "proxy.stderr.log");
            File.WriteAllText(log, "{\"time\":\"" + now.ToString("o") + "\",\"msg\":\"stats\",\"gateway\":{\"packets_in\":25,\"packets_out\":12,\"active\":3}}\n{\"partial");
            var reader = new GatewayReader(root); var snapshot = reader.Read(now);
            Check(snapshot.Ready && snapshot.HasStats && snapshot.In == 25 && snapshot.Active == 3, "Partial log tail does not hide last complete sample");
            File.SetLastWriteTimeUtc(manifest, now.AddMinutes(-1)); snapshot = reader.Read(now);
            Check(!snapshot.Ready, "Old heartbeat is not healthy");
            File.WriteAllText(manifest, "{broken"); Check(reader.Read(now).State == "unavailable", "Corrupt state fails visibly");
            File.Delete(manifest); Check(reader.Read(now).State == "stopped", "Absent state is stopped");
            Check(Json.Time("2026-09-20T02:00:00.123456789+08:00") != DateTime.MinValue, "Go nanosecond timestamps accepted");
            Console.WriteLine("Passed " + checks + " desktop model checks.");
        }
        finally { if (Directory.Exists(root)) Directory.Delete(root, true); }
    }
}
