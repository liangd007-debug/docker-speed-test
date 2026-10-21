// DockerSpeedTestGUI —— Docker 镜像加速测速图形界面
// 调用同目录 DockerSpeedTest.exe -json（NDJSON 事件流）解析显示。
// 语法限定 C#5（系统 csc v4.0.30319），无字符串插值、无 ?. 。
using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Text;
using System.Web.Script.Serialization;
using System.Windows.Forms;

namespace DockerSpeedTestGui
{
    public class MainForm : Form
    {
        private TextBox txtImage;
        private Button btnStart;
        private Button btnStop;
        private Label lblStatus;

        private TextBox txtInput;
        private Button btnAdd;
        private Button btnDel;
        private Button btnClear;
        private ListBox lst;
        private Label lblListPath;

        private DataGridView dgv;
        private Panel pnlCmd;
        private RichTextBox txtCmd;      // 中部共用视图：默认运行日志，点复制按钮切换为命令
        private Button btnCopyLinux;
        private Button btnCopyJson;
        private Button btnCopyWin;
        private Button btnCopyPull;
        private Button btnViewLog;
        private ProgressBar progress;
        private readonly List<KeyValuePair<string, Color>> logLines =
            new List<KeyValuePair<string, Color>>();
        private bool logMode = true;     // true=日志视图, false=命令视图

        private Process proc;
        private bool running;
        private string listPath;
        private string exePath;
        private int sortDesc; // 0=默认(测完降序) 1=降序 2=升序

        private static readonly string[] DefaultMirrors = new string[]
        {
            "https://docker.1ms.run",
            "https://docker.xuanyuan.me",
            "https://docker.m.daocloud.io",
            "https://docker.1panel.live",
            "https://dockerproxy.net",
            "https://dockerproxy.link",
            "https://proxy.vvvv.ee",
            "https://docker.jiaxin.site",
            "https://registry.cyou",
            "https://mirror.houlang.cloud",
            "https://xuanyuan.cloud",
            "https://atomhub.openatom.cn",
            "https://hub.uuuadc.top",
            "https://docker.hpcloud.cloud",
            "https://docker-0.unsee.tech",
            "https://public.ecr.aws",
            "https://cr.yandex",
            "https://hubfast.cn",
            "https://free.hubfast.cn"
        };

        public MainForm()
        {
            Text = "Docker 加速测速 GUI —— 免 Docker 实测镜像拉取速度";
            Width = 1100;
            Height = 700;
            StartPosition = FormStartPosition.CenterScreen;
            Font = new Font("Microsoft YaHei UI", 9F);
            MinimumSize = new Size(960, 580);

            exePath = Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "DockerSpeedTest.exe");
            listPath = Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "mirrors.txt");

            BuildUI();
            SetLogView();
            LoadList();

            FormClosing += MainForm_FormClosing;
        }

        // ---------------- 界面搭建 ----------------

        private void BuildUI()
        {
            // ---- 顶部工具条 ----
            Panel pnlTop = new Panel();
            pnlTop.Dock = DockStyle.Top;
            pnlTop.Height = 44;
            pnlTop.Padding = new Padding(6, 7, 6, 5);

            Label lblImage = new Label();
            lblImage.Text = "测试镜像:";
            lblImage.Dock = DockStyle.Left;
            lblImage.AutoSize = true;
            lblImage.TextAlign = ContentAlignment.MiddleLeft;
            lblImage.Padding = new Padding(0, 0, 8, 0);

            txtImage = new TextBox();
            txtImage.Dock = DockStyle.Left;
            txtImage.Width = 180;
            txtImage.Text = "busybox:latest";

            btnStart = new Button();
            btnStart.Text = "▶ 开始测试";
            btnStart.Dock = DockStyle.Left;
            btnStart.Width = 110;
            btnStart.Margin = new Padding(6, 0, 0, 0);
            btnStart.BackColor = Color.FromArgb(0, 138, 70);
            btnStart.ForeColor = Color.White;
            btnStart.FlatStyle = FlatStyle.Flat;
            btnStart.FlatAppearance.BorderSize = 0;
            btnStart.Cursor = Cursors.Hand;
            btnStart.Click += btnStart_Click;

            btnStop = new Button();
            btnStop.Text = "■ 停止";
            btnStop.Dock = DockStyle.Left;
            btnStop.Width = 80;
            btnStop.Margin = new Padding(6, 0, 0, 0);
            btnStop.Enabled = false;
            btnStop.BackColor = Color.FromArgb(170, 60, 55);
            btnStop.ForeColor = Color.White;
            btnStop.FlatStyle = FlatStyle.Flat;
            btnStop.FlatAppearance.BorderSize = 0;
            btnStop.Cursor = Cursors.Hand;
            btnStop.Click += btnStop_Click;

            lblStatus = new Label();
            lblStatus.Text = "就绪 —— 在左侧输入加速站地址，点「开始测试」";
            lblStatus.Dock = DockStyle.Fill;
            lblStatus.TextAlign = ContentAlignment.MiddleLeft;
            lblStatus.Padding = new Padding(12, 0, 0, 0);
            lblStatus.ForeColor = Color.FromArgb(70, 70, 70);

            // Dock 布局忽略 Margin，控件间距用空白 Label 占位
            // （镜像输入框与「开始测试」之间留 10px，两按钮间留 8px）
            Label spA = new Label();
            spA.Dock = DockStyle.Left;
            spA.Width = 10;
            spA.Text = "";
            Label spB = new Label();
            spB.Dock = DockStyle.Left;
            spB.Width = 8;
            spB.Text = "";

            // 后加的先占边：Add 顺序即从右到左（lblImage 最后加 → 最左）
            pnlTop.Controls.Add(lblStatus);
            pnlTop.Controls.Add(btnStop);
            pnlTop.Controls.Add(spB);
            pnlTop.Controls.Add(btnStart);
            pnlTop.Controls.Add(spA);
            pnlTop.Controls.Add(txtImage);
            pnlTop.Controls.Add(lblImage);

            // ---- 左侧：镜像列表 ----
            Panel pnlLeft = new Panel();
            pnlLeft.Dock = DockStyle.Left;
            pnlLeft.Width = 315;
            pnlLeft.Padding = new Padding(6, 4, 6, 4);

            Label lblTip = new Label();
            lblTip.Text = "镜像加速站列表（可直接输入，每行一个）:";
            lblTip.Dock = DockStyle.Top;
            lblTip.Height = 22;
            lblTip.TextAlign = ContentAlignment.MiddleLeft;

            txtInput = new TextBox();
            txtInput.Dock = DockStyle.Top;
            txtInput.Multiline = true;
            txtInput.Height = 92;
            txtInput.ScrollBars = ScrollBars.Vertical;
            txtInput.AcceptsReturn = true;
            txtInput.Text = "";
            // 提示文案画在上方 Label，不使用 PlaceHolderText（Framework 4.x 没有该属性）

            Panel pnlBtns = new Panel();
            pnlBtns.Dock = DockStyle.Top;
            pnlBtns.Height = 34;
            pnlBtns.Padding = new Padding(0, 3, 0, 2);

            btnAdd = new Button();
            btnAdd.Text = "添加";
            btnAdd.Dock = DockStyle.Left;
            btnAdd.Width = 70;
            btnAdd.Click += btnAdd_Click;

            btnDel = new Button();
            btnDel.Text = "删除选中";
            btnDel.Dock = DockStyle.Left;
            btnDel.Width = 86;
            btnDel.Margin = new Padding(6, 0, 0, 0);
            btnDel.Click += btnDel_Click;

            btnClear = new Button();
            btnClear.Text = "清空";
            btnClear.Dock = DockStyle.Left;
            btnClear.Width = 70;
            btnClear.Margin = new Padding(6, 0, 0, 0);
            btnClear.Click += btnClear_Click;

            pnlBtns.Controls.Add(btnClear);
            pnlBtns.Controls.Add(btnDel);
            pnlBtns.Controls.Add(btnAdd);

            lblListPath = new Label();
            lblListPath.Dock = DockStyle.Bottom;
            lblListPath.Height = 20;
            lblListPath.ForeColor = Color.Gray;
            lblListPath.Font = new Font(Font.FontFamily, 7.5F);
            lblListPath.TextAlign = ContentAlignment.MiddleLeft;

            lst = new ListBox();
            lst.Dock = DockStyle.Fill;
            lst.IntegralHeight = false;

            // Add 顺序：Fill 最先(z最低)，边框后加先占
            pnlLeft.Controls.Add(lst);
            pnlLeft.Controls.Add(lblListPath);
            pnlLeft.Controls.Add(pnlBtns);
            pnlLeft.Controls.Add(txtInput);
            pnlLeft.Controls.Add(lblTip);

            // ---- 底部：进度条（原独立运行日志框已并入中部视图） ----
            Panel pnlProg = new Panel();
            pnlProg.Dock = DockStyle.Bottom;
            pnlProg.Height = 24;
            pnlProg.Padding = new Padding(6, 4, 6, 4);

            progress = new ProgressBar();
            progress.Dock = DockStyle.Fill;
            progress.Style = ProgressBarStyle.Continuous;
            progress.Minimum = 0;
            progress.Maximum = 100;
            progress.Value = 0;

            pnlProg.Controls.Add(progress);

            // ---- 中央：结果表格 + 命令区 ----
            Panel pnlCenter = new Panel();
            pnlCenter.Dock = DockStyle.Fill;
            pnlCenter.Padding = new Padding(6, 4, 6, 0);

            dgv = new DataGridView();
            dgv.Dock = DockStyle.Fill;
            dgv.AllowUserToAddRows = false;
            dgv.AllowUserToDeleteRows = false;
            dgv.AllowUserToResizeRows = false;
            dgv.ReadOnly = true;
            dgv.RowHeadersVisible = false;
            dgv.SelectionMode = DataGridViewSelectionMode.FullRowSelect;
            dgv.MultiSelect = false;
            dgv.AutoSizeColumnsMode = DataGridViewAutoSizeColumnsMode.None;
            dgv.BackgroundColor = Color.White;
            dgv.BorderStyle = BorderStyle.FixedSingle;
            dgv.RowTemplate.Height = 24;

            AddCol("rank", "排名", 52, true);
            AddCol("host", "镜像源", 210, true);
            AddCol("status", "状态", 66, true);
            AddCol("manifest", "清单延迟", 88, true);
            AddCol("dl", "下载耗时", 88, true);
            AddCol("size", "大小", 84, true);
            AddCol("speed", "速度 MB/s", 96, true);
            AddCol("note", "说明", 200, true);
            DataGridViewTextBoxColumn colUrl = AddCol("url", "URL", 10, false);
            colUrl.Visible = false;
            dgv.CellDoubleClick += dgv_CellDoubleClick;
            dgv.ColumnHeaderMouseClick += dgv_ColumnHeaderMouseClick;

            pnlCmd = new Panel();
            pnlCmd.Dock = DockStyle.Bottom;
            pnlCmd.Height = 176;
            pnlCmd.Padding = new Padding(0, 4, 0, 0);

            Label lblCmd = new Label();
            lblCmd.Text = "点「运行日志」看输出；点选表格行后点复制按钮，生成的命令显示在这里:";
            lblCmd.Dock = DockStyle.Top;
            lblCmd.Height = 22;
            lblCmd.TextAlign = ContentAlignment.MiddleLeft;

            // FlowLayoutPanel 里 Margin 才生效（普通 Panel 的 Dock 布局会忽略 Margin）
            FlowLayoutPanel pnlCmdBtns = new FlowLayoutPanel();
            pnlCmdBtns.Dock = DockStyle.Top;
            pnlCmdBtns.Height = 40;
            pnlCmdBtns.FlowDirection = FlowDirection.LeftToRight;
            pnlCmdBtns.WrapContents = false;
            pnlCmdBtns.Padding = new Padding(0, 5, 0, 2);

            btnViewLog = new Button();
            btnViewLog.Text = "运行日志";
            // FlowLayoutPanel 内禁用 Dock（Dock 会把按钮高度压成 0），靠加入顺序+Margin 排
            btnViewLog.Width = 96;
            btnViewLog.Height = 30;
            btnViewLog.Margin = new Padding(0, 0, 8, 0);
            btnViewLog.Click += btnViewLog_Click;

            btnCopyLinux = new Button();
            btnCopyLinux.Text = "复制 Linux 一键写入";
            btnCopyLinux.Width = 150;
            btnCopyLinux.Height = 30;
            btnCopyLinux.Margin = new Padding(0, 0, 8, 0);
            btnCopyLinux.Click += btnCopyLinux_Click;

            btnCopyJson = new Button();
            btnCopyJson.Text = "复制 daemon.json";
            btnCopyJson.Width = 140;
            btnCopyJson.Height = 30;
            btnCopyJson.Margin = new Padding(0, 0, 8, 0);
            btnCopyJson.Click += btnCopyJson_Click;

            btnCopyWin = new Button();
            btnCopyWin.Text = "复制 Windows 配置";
            btnCopyWin.Width = 150;
            btnCopyWin.Height = 30;
            btnCopyWin.Margin = new Padding(0, 0, 8, 0);
            btnCopyWin.Click += btnCopyWin_Click;

            btnCopyPull = new Button();
            btnCopyPull.Text = "复制 docker pull 前缀命令";
            btnCopyPull.Width = 175;
            btnCopyPull.Height = 30;
            btnCopyPull.Margin = new Padding(0, 0, 0, 0);
            btnCopyPull.Click += btnCopyPull_Click;

            // FlowLayoutPanel 按 Controls 顺序从左排（与 Dock 的“后加先左”相反）
            pnlCmdBtns.Controls.Add(btnViewLog);    // 最左：运行日志
            pnlCmdBtns.Controls.Add(btnCopyLinux);
            pnlCmdBtns.Controls.Add(btnCopyJson);
            pnlCmdBtns.Controls.Add(btnCopyWin);
            pnlCmdBtns.Controls.Add(btnCopyPull);

            txtCmd = new RichTextBox();
            txtCmd.Dock = DockStyle.Fill;
            txtCmd.ReadOnly = true;
            txtCmd.ScrollBars = RichTextBoxScrollBars.Vertical;
            txtCmd.Font = new Font("Consolas", 9F);
            txtCmd.BackColor = Color.FromArgb(246, 248, 250);
            txtCmd.BorderStyle = BorderStyle.FixedSingle;
            txtCmd.Text = "";

            pnlCmd.Controls.Add(txtCmd);
            pnlCmd.Controls.Add(pnlCmdBtns);
            pnlCmd.Controls.Add(lblCmd);

            pnlCenter.Controls.Add(dgv);
            pnlCenter.Controls.Add(pnlCmd);

            // ---- 窗体装配（Fill 最先加） ----
            Controls.Add(pnlCenter);
            Controls.Add(pnlLeft);
            Controls.Add(pnlProg);
            Controls.Add(pnlTop);
        }

        private DataGridViewTextBoxColumn AddCol(string name, string title, int width, bool visible)
        {
            DataGridViewTextBoxColumn col = new DataGridViewTextBoxColumn();
            col.Name = name;
            col.HeaderText = title;
            col.Width = width;
            col.Visible = visible;
            col.SortMode = DataGridViewColumnSortMode.NotSortable;
            dgv.Columns.Add(col);
            return col;
        }

        // ---------------- 列表读写 ----------------

        private void LoadList()
        {
            try
            {
                if (!File.Exists(listPath))
                {
                    File.WriteAllText(listPath,
                        string.Join(Environment.NewLine, DefaultMirrors),
                        new UTF8Encoding(false));
                }
                string[] lines = File.ReadAllLines(listPath, Encoding.UTF8);
                foreach (string raw in lines)
                {
                    string u = NormalizeLine(raw);
                    if (u.Length > 0 && !ListHas(u))
                    {
                        lst.Items.Add(u);
                    }
                }
            }
            catch (Exception ex)
            {
                Log("读取列表失败: " + ex.Message, Color.Red);
            }
            lblListPath.Text = listPath;
        }

        private void SaveList()
        {
            StringBuilder sb = new StringBuilder();
            foreach (object o in lst.Items)
            {
                if (sb.Length > 0)
                {
                    sb.Append(Environment.NewLine);
                }
                sb.Append((string)o);
            }
            File.WriteAllText(listPath, sb.ToString(), new UTF8Encoding(false));
        }

        // 与 CLI 相同的宽容解析：@url:`...` / 引号 / 逗号 / 注释 / 缺协议
        private string NormalizeLine(string line)
        {
            string s = (line == null) ? "" : line.Trim().Trim('\uFEFF');
            if (s.Length == 0 || s.StartsWith("#"))
            {
                return "";
            }
            int ci = s.IndexOf(" #");
            if (ci >= 0)
            {
                s = s.Substring(0, ci).Trim();
            }
            string low = s.ToLowerInvariant();
            if (low.StartsWith("@url:"))
            {
                s = s.Substring(5).Trim();
            }
            else if (low.StartsWith("url:"))
            {
                s = s.Substring(4).Trim();
            }
            s = s.Trim().Trim('`', '\'', '"').Trim();
            if (s.EndsWith(","))
            {
                s = s.Substring(0, s.Length - 1).Trim().Trim('`', '\'', '"').Trim();
            }
            if (s.Length == 0 || s.IndexOf(' ') >= 0 || s.IndexOf('@') >= 0)
            {
                return "";
            }
            if (s.IndexOf("://") < 0)
            {
                s = "https://" + s;
            }
            Uri u;
            if (!Uri.TryCreate(s, UriKind.Absolute, out u) ||
                (u.Scheme != "http" && u.Scheme != "https") || u.Host.Length == 0)
            {
                return "";
            }
            return s.TrimEnd('/');
        }

        private bool ListHas(string url)
        {
            foreach (object o in lst.Items)
            {
                if (string.Equals((string)o, url, StringComparison.OrdinalIgnoreCase))
                {
                    return true;
                }
            }
            return false;
        }

        // ---------------- 按钮事件 ----------------

        private void btnAdd_Click(object sender, EventArgs e)
        {
            string[] lines = txtInput.Text.Split(
                new string[] { "\r\n", "\n" }, StringSplitOptions.RemoveEmptyEntries);
            int added = 0;
            foreach (string line in lines)
            {
                string u = NormalizeLine(line);
                if (u.Length > 0 && !ListHas(u))
                {
                    lst.Items.Add(u);
                    added++;
                }
            }
            txtInput.Clear();
            if (added > 0)
            {
                Log("已添加 " + added + " 个镜像源", Color.Green);
            }
        }

        private void btnDel_Click(object sender, EventArgs e)
        {
            for (int i = lst.SelectedIndices.Count - 1; i >= 0; i--)
            {
                lst.Items.RemoveAt(lst.SelectedIndices[i]);
            }
        }

        private void btnClear_Click(object sender, EventArgs e)
        {
            if (lst.Items.Count > 0 &&
                MessageBox.Show("清空整个镜像列表？", "确认",
                    MessageBoxButtons.YesNo, MessageBoxIcon.Question) == DialogResult.Yes)
            {
                lst.Items.Clear();
            }
        }

        private void btnStart_Click(object sender, EventArgs e)
        {
            if (running)
            {
                return;
            }
            if (!File.Exists(exePath))
            {
                MessageBox.Show("未找到测速引擎：\n" + exePath +
                    "\n请把 DockerSpeedTestGUI.exe 与 DockerSpeedTest.exe 放在同一目录。",
                    "缺少文件", MessageBoxButtons.OK, MessageBoxIcon.Warning);
                return;
            }
            if (lst.Items.Count == 0)
            {
                MessageBox.Show("列表为空，请先在左侧输入要测试的加速站地址。", "提示",
                    MessageBoxButtons.OK, MessageBoxIcon.Information);
                return;
            }
            string image = txtImage.Text.Trim();
            if (image.Length == 0)
            {
                image = "busybox:latest";
                txtImage.Text = image;
            }
            try
            {
                SaveList();
            }
            catch (Exception ex)
            {
                MessageBox.Show("保存列表失败: " + ex.Message, "错误",
                    MessageBoxButtons.OK, MessageBoxIcon.Error);
                return;
            }

            dgv.Rows.Clear();
            logLines.Clear();
            SetLogView();
            progress.Value = 0;
            sortDesc = 0;

            ProcessStartInfo psi = new ProcessStartInfo();
            psi.FileName = exePath;
            psi.Arguments = string.Format("-json -nopause -list \"{0}\" -image \"{1}\"",
                listPath, image);
            psi.UseShellExecute = false;
            psi.RedirectStandardOutput = true;
            psi.RedirectStandardError = true;
            psi.RedirectStandardInput = true;
            psi.CreateNoWindow = true;
            psi.StandardOutputEncoding = new UTF8Encoding(false);
            psi.StandardErrorEncoding = new UTF8Encoding(false);

            proc = new Process();
            proc.StartInfo = psi;
            proc.EnableRaisingEvents = true;
            proc.OutputDataReceived += proc_OutputDataReceived;
            proc.ErrorDataReceived += proc_ErrorDataReceived;
            proc.Exited += proc_Exited;

            try
            {
                proc.Start();
                proc.BeginOutputReadLine();
                proc.BeginErrorReadLine();
            }
            catch (Exception ex)
            {
                MessageBox.Show("启动失败: " + ex.Message, "错误",
                    MessageBoxButtons.OK, MessageBoxIcon.Error);
                proc = null;
                return;
            }

            running = true;
            btnStart.Enabled = false;
            btnStop.Enabled = true;
            lblStatus.Text = "测试中…（阶段1 连通性检测 → 阶段2 拉取测速）";
            lblStatus.ForeColor = Color.FromArgb(0, 100, 180);
            Log("启动: " + Path.GetFileName(exePath) + " -json  镜像=" + image, Color.DarkBlue);
        }

        private void btnStop_Click(object sender, EventArgs e)
        {
            StopProcess("用户停止");
        }

        private void StopProcess(string reason)
        {
            try
            {
                if (proc != null && !proc.HasExited)
                {
                    proc.Kill();
                }
            }
            catch
            {
                // 忽略：进程可能刚好自己结束
            }
            Log(reason, Color.Red);
        }

        // ---------------- 子进程输出处理 ----------------

        private void proc_OutputDataReceived(object sender, DataReceivedEventArgs e)
        {
            string line = e.Data;
            if (line == null || line.Trim().Length == 0)
            {
                return;
            }
            if (InvokeRequired)
            {
                BeginInvoke(new Action<string>(HandleLine), new object[] { line });
            }
            else
            {
                HandleLine(line);
            }
        }

        private void proc_ErrorDataReceived(object sender, DataReceivedEventArgs e)
        {
            if (e.Data != null && e.Data.Trim().Length > 0)
            {
                BeginInvoke(new Action<string>(LogErr), new object[] { e.Data });
            }
        }

        private void LogErr(string s)
        {
            Log("[stderr] " + s, Color.Red);
        }

        private void proc_Exited(object sender, EventArgs e)
        {
            if (InvokeRequired)
            {
                BeginInvoke(new Action(OnExitedUi));
            }
            else
            {
                OnExitedUi();
            }
        }

        private void OnExitedUi()
        {
            if (!running)
            {
                return;
            }
            running = false;
            btnStart.Enabled = true;
            btnStop.Enabled = false;
            if (lblStatus.ForeColor != Color.FromArgb(200, 40, 40))
            {
                lblStatus.Text = "测试结束";
                lblStatus.ForeColor = Color.FromArgb(70, 70, 70);
            }
        }

        private void HandleLine(string line)
        {
            Dictionary<string, object> d;
            try
            {
                JavaScriptSerializer js = new JavaScriptSerializer();
                js.MaxJsonLength = 1024 * 1024;
                d = js.DeserializeObject(line) as Dictionary<string, object>;
            }
            catch
            {
                Log("[raw] " + line, Color.Gray);
                return;
            }
            if (d == null)
            {
                Log("[raw] " + line, Color.Gray);
                return;
            }
            string t = GetString(d, "t");
            try
            {
                if (t == "start")
                {
                    Log("==== 开始测试  镜像 " + GetString(d, "image") +
                        "，共 " + GetInt(d, "total") + " 个加速源 ====", Color.DarkBlue);
                }
                else if (t == "p1")
                {
                    OnP1(d);
                }
                else if (t == "p2")
                {
                    OnP2(d);
                }
                else if (t == "done")
                {
                    OnDone(d);
                }
                else if (t == "error")
                {
                    Log("错误: " + GetString(d, "msg"), Color.Red);
                    lblStatus.Text = "出错";
                    lblStatus.ForeColor = Color.FromArgb(200, 40, 40);
                }
            }
            catch (Exception ex)
            {
                Log("处理事件异常: " + ex.Message, Color.Red);
            }
        }

        private void OnP1(Dictionary<string, object> d)
        {
            string url = GetString(d, "url");
            string host = GetString(d, "host");
            bool ok = GetBool(d, "ok");
            long ms = GetLong(d, "ms");
            long layers = GetLong(d, "layers");
            string detail = GetString(d, "detail");
            int n = GetInt(d, "n");
            int total = GetInt(d, "total");

            int ri = dgv.Rows.Add(
                "", host, ok ? "可达" : "失败",
                ok ? ms + "ms" : "", "", "", "", detail, url);
            DataGridViewRow row = dgv.Rows[ri];
            if (ok)
            {
                row.Cells["status"].Style.ForeColor = Color.FromArgb(0, 130, 60);
            }
            else
            {
                row.Cells["status"].Style.ForeColor = Color.FromArgb(200, 40, 40);
            }

            string tag = "[" + n + "/" + total + "] " + host + "  " +
                (ok ? "OK  清单" + ms + "ms, 层" + layers : "FAIL  " + detail);
            Log(tag, ok ? Color.Green : Color.Red);
            if (total > 0)
            {
                progress.Value = Math.Max(progress.Value, (int)(n * 50.0 / total));
            }
        }

        private void OnP2(Dictionary<string, object> d)
        {
            string url = GetString(d, "url");
            bool ok = GetBool(d, "ok");
            string detail = GetString(d, "detail");
            double speed = GetDouble(d, "speed");
            long bytes = GetLong(d, "bytes");
            double sec = GetDouble(d, "sec");
            long ms = GetLong(d, "ms");
            int n = GetInt(d, "n");
            int total = GetInt(d, "total");

            DataGridViewRow row = FindRow(url);
            if (row == null)
            {
                int ri = dgv.Rows.Add("", GetString(d, "host"), "", "", "", "", "", "", url);
                row = dgv.Rows[ri];
            }

            row.Cells["manifest"].Value = ms > 0 ? ms + "ms" : "";
            if (ok)
            {
                row.Cells["status"].Value = "可用";
                row.Cells["status"].Style.ForeColor = Color.FromArgb(0, 130, 60);
                row.Cells["dl"].Value = Math.Round(sec, 2) + "s";
                row.Cells["size"].Value = HumanBytes(bytes);
                row.Cells["speed"].Value = Math.Round(speed, 2).ToString("0.00");
                row.Cells["note"].Value = detail;
                Log("[" + n + "/" + total + "] " + GetString(d, "host") + "  " +
                    Math.Round(speed, 2).ToString("0.00") + " MB/s  " +
                    HumanBytes(bytes) + "  " + Math.Round(sec, 2) + "s", Color.Green);
            }
            else
            {
                row.Cells["status"].Value = "失败";
                row.Cells["status"].Style.ForeColor = Color.FromArgb(200, 40, 40);
                row.Cells["note"].Value = detail;
                Log("[" + n + "/" + total + "] " + GetString(d, "host") + "  测速失败  " +
                    detail, Color.Red);
            }
            if (total > 0)
            {
                int pct = 50 + (int)(n * 50.0 / total);
                progress.Value = Math.Max(progress.Value, Math.Min(100, pct));
            }
        }

        private void OnDone(Dictionary<string, object> d)
        {
            int ok = GetInt(d, "ok");
            int fail = GetInt(d, "fail");
            double totalSec = GetDouble(d, "total_sec");
            string best = GetString(d, "best");
            double bestSpeed = GetDouble(d, "best_speed");
            string csv = GetString(d, "csv");

            SortRows(true);

            lblStatus.Text = string.Format("完成：可用 {0} / 失败 {1}，用时 {2}，最快 {3} {4:0.00} MB/s",
                ok, fail, Math.Round(totalSec, 1) + "s", best, bestSpeed);
            lblStatus.ForeColor = Color.FromArgb(0, 130, 60);

            StringBuilder sb = new StringBuilder();
            sb.AppendLine("==== 测速完成：可用 " + ok + " / 失败 " + fail +
                "，总耗时 " + Math.Round(totalSec, 1) + "s ====");
            if (ok > 0)
            {
                sb.AppendLine("推荐: " + best + "  " +
                    Math.Round(bestSpeed, 2).ToString("0.00") + " MB/s");
                sb.AppendLine("在表格里点选一行 → 点下方按钮复制「一键写入命令」。");
            }
            if (csv.Length > 0)
            {
                sb.AppendLine("CSV 结果: " + csv);
            }
            Log(sb.ToString(), Color.DarkBlue);
            progress.Value = 100;
        }

        // ---------------- 排序与选中 ----------------

        private void SortRows(bool desc)
        {
            List<object[]> okRows = new List<object[]>();
            List<object[]> badRows = new List<object[]>();
            foreach (DataGridViewRow row in dgv.Rows)
            {
                object[] vals = new object[9];
                for (int i = 0; i < 9; i++)
                {
                    vals[i] = row.Cells[i].Value;
                }
                double sp = 0;
                double.TryParse((string)vals[6], out sp);
                if (sp > 0)
                {
                    okRows.Add(vals);
                }
                else
                {
                    badRows.Add(vals);
                }
            }
            okRows.Sort(delegate(object[] a, object[] b)
            {
                double x = 0;
                double y = 0;
                double.TryParse((string)a[6], out x);
                double.TryParse((string)b[6], out y);
                return desc ? y.CompareTo(x) : x.CompareTo(y);
            });

            dgv.Rows.Clear();
            int rank = 0;
            foreach (object[] vals in okRows)
            {
                rank++;
                vals[0] = rank.ToString();
                int ri = dgv.Rows.Add(vals);
                ColorRows(ri, vals);
            }
            foreach (object[] vals in badRows)
            {
                rank++;
                vals[0] = rank.ToString();
                int ri = dgv.Rows.Add(vals);
                ColorRows(ri, vals);
            }
            if (dgv.Rows.Count > 0)
            {
                dgv.Rows[0].Selected = true;
            }
        }

        private void ColorRows(int ri, object[] vals)
        {
            DataGridViewRow row = dgv.Rows[ri];
            string st = (string)vals[2];
            if (st == "可用")
            {
                row.Cells["status"].Style.ForeColor = Color.FromArgb(0, 130, 60);
            }
            else if (st == "失败")
            {
                row.Cells["status"].Style.ForeColor = Color.FromArgb(200, 40, 40);
            }
            else
            {
                row.Cells["status"].Style.ForeColor = Color.FromArgb(180, 120, 0);
            }
        }

        private DataGridViewRow FindRow(string url)
        {
            foreach (DataGridViewRow row in dgv.Rows)
            {
                if (string.Equals((string)row.Cells["url"].Value, url,
                    StringComparison.OrdinalIgnoreCase))
                {
                    return row;
                }
            }
            return null;
        }

        private void dgv_CellDoubleClick(object sender, DataGridViewCellEventArgs e)
        {
            if (e.RowIndex >= 0)
            {
                CopyText(BuildLinuxOneLiner(GetSelectedUrl()), "Linux 一键写入命令");
            }
        }

        private void dgv_ColumnHeaderMouseClick(object sender, DataGridViewCellMouseEventArgs e)
        {
            if (dgv.Columns[e.ColumnIndex].Name == "speed")
            {
                bool desc = sortDesc != 2;
                sortDesc = desc ? 1 : 2;
                SortRows(desc);
            }
        }

        private string GetSelectedUrl()
        {
            if (dgv.SelectedRows.Count == 0)
            {
                return "";
            }
            return (string)dgv.SelectedRows[0].Cells["url"].Value;
        }

        private void UpdateCmdBox()   // 保留为空实现：选中行不再自动覆盖视图，由复制按钮/运行日志按钮切换
        {
        }

        // ---------------- 命令构造 ----------------

        private string BuildLinuxOneLiner(string url)
        {
            return "echo '" + BuildDaemonJson(url) + "' | sudo tee /etc/docker/daemon.json " +
                "&& sudo systemctl daemon-reload && sudo systemctl restart docker";
        }

        private string BuildDaemonJson(string url)
        {
            return "{\"registry-mirrors\": [\"" + url + "\"]}";
        }

        private string BuildWinJson(string url)
        {
            return "{\"registryMirrors\": [\"" + url + "\"]}";
        }

        private string BuildPullCmd(string url)
        {
            string image = txtImage.Text.Trim();
            if (image.Length == 0)
            {
                image = "busybox:latest";
            }
            string repo = image;
            string tag = "latest";
            int ci = image.LastIndexOf(':');
            int si = image.LastIndexOf('/');
            if (ci > 0 && ci > si)
            {
                tag = image.Substring(ci + 1);
                repo = image.Substring(0, ci);
            }
            if (repo.IndexOf('/') < 0)
            {
                repo = "library/" + repo;
            }
            string host = url;
            try
            {
                host = new Uri(url).Host;
            }
            catch
            {
                // 保留原值
            }
            return "docker pull " + host + "/" + repo + ":" + tag;
        }

        private void btnCopyLinux_Click(object sender, EventArgs e)
        {
            CopyForced(BuildLinuxOneLiner, "Linux 一键写入命令");
        }

        private void btnCopyJson_Click(object sender, EventArgs e)
        {
            CopyForced(BuildDaemonJson, "daemon.json 内容");
        }

        private void btnCopyWin_Click(object sender, EventArgs e)
        {
            CopyForced(BuildWinJson, "Windows Docker Desktop 配置");
        }

        private void btnCopyPull_Click(object sender, EventArgs e)
        {
            CopyForced(BuildPullCmd, "docker pull 前缀命令");
        }

        private delegate string CmdBuilder(string url);

        private void CopyForced(CmdBuilder build, string what)
        {
            string url = GetSelectedUrl();
            if (url.Length == 0)
            {
                MessageBox.Show("请先在上方表格点选一个镜像源。", "提示",
                    MessageBoxButtons.OK, MessageBoxIcon.Information);
                return;
            }
            CopyText(build(url), what);
        }

        private void CopyText(string text, string what)
        {
            try
            {
                Clipboard.SetText(text);
                Log("已复制" + what, Color.DarkBlue);
                SetCmdView(text);   // 下方视图切换为这条命令的内容
                lblStatus.Text = "已复制" + what + "（内容显示在下方），粘贴到终端执行即可";
                lblStatus.ForeColor = Color.FromArgb(0, 130, 60);
            }
            catch (Exception ex)
            {
                Log("复制失败: " + ex.Message, Color.Red);
            }
        }

        // ---------------- 工具 ----------------

        // ---------------- 视图：运行日志 / 生成命令（同一个框二选一显示） ----------------

        private void Log(string s, Color c)
        {
            if (txtCmd.IsDisposed)
            {
                return;
            }
            logLines.Add(new KeyValuePair<string, Color>(s, c));
            if (logLines.Count > 800)
            {
                logLines.RemoveRange(0, 200);
            }
            if (logMode)
            {
                AppendColored(s, c);
            }
        }

        private void AppendColored(string s, Color c)
        {
            txtCmd.SelectionStart = txtCmd.TextLength;
            txtCmd.SelectionColor = c;
            txtCmd.AppendText(s + Environment.NewLine);
            txtCmd.SelectionColor = txtCmd.ForeColor;
            txtCmd.ScrollToCaret();
            if (txtCmd.TextLength > 30000)
            {
                txtCmd.Text = txtCmd.Text.Substring(txtCmd.TextLength - 20000);
                txtCmd.SelectionStart = txtCmd.TextLength;
                txtCmd.ScrollToCaret();
            }
        }

        // 切回运行日志视图（重放全部历史行）
        private void SetLogView()
        {
            logMode = true;
            txtCmd.Clear();
            foreach (KeyValuePair<string, Color> kv in logLines)
            {
                AppendColored(kv.Key, kv.Value);
            }
            HighlightViewButtons();
        }

        // 切到命令视图，显示指定命令文本
        private void SetCmdView(string text)
        {
            logMode = false;
            txtCmd.Text = text;
            txtCmd.SelectionStart = 0;
            txtCmd.SelectionColor = txtCmd.ForeColor;
            HighlightViewButtons();
        }

        private void HighlightViewButtons()
        {
            btnViewLog.BackColor = logMode ? Color.FromArgb(0, 130, 60) : Color.FromArgb(232, 232, 232);
            btnViewLog.ForeColor = logMode ? Color.White : Color.Black;
        }

        private void btnViewLog_Click(object sender, EventArgs e)
        {
            SetLogView();
        }

        private static string GetString(Dictionary<string, object> d, string k)
        {
            object v;
            if (d.TryGetValue(k, out v) && v != null)
            {
                return v.ToString();
            }
            return "";
        }

        private static double GetDouble(Dictionary<string, object> d, string k)
        {
            object v;
            if (d.TryGetValue(k, out v) && v != null)
            {
                if (v is double)
                {
                    return (double)v;
                }
                if (v is int)
                {
                    return (int)v;
                }
                if (v is decimal)
                {
                    return (double)(decimal)v;
                }
                double res;
                double.TryParse(v.ToString(), out res);
                return res;
            }
            return 0;
        }

        private static long GetLong(Dictionary<string, object> d, string k)
        {
            return (long)Math.Round(GetDouble(d, k));
        }

        private static int GetInt(Dictionary<string, object> d, string k)
        {
            return (int)Math.Round(GetDouble(d, k));
        }

        private static bool GetBool(Dictionary<string, object> d, string k)
        {
            object v;
            if (d.TryGetValue(k, out v) && v is bool)
            {
                return (bool)v;
            }
            return false;
        }

        private static string HumanBytes(long n)
        {
            if (n >= 1073741824L)
            {
                return Math.Round((double)n / 1073741824.0, 2) + "GB";
            }
            if (n >= 1048576L)
            {
                return Math.Round((double)n / 1048576.0, 2) + "MB";
            }
            if (n >= 1024L)
            {
                return Math.Round((double)n / 1024.0, 1) + "KB";
            }
            return n + "B";
        }

        private void MainForm_FormClosing(object sender, FormClosingEventArgs e)
        {
            StopProcess("窗口关闭，停止测试");
        }
    }

    public static class Program
    {
        [STAThread]
        public static void Main()
        {
            Application.EnableVisualStyles();
            Application.SetCompatibleTextRenderingDefault(false);
            Application.Run(new MainForm());
        }
    }
}
