import Link from "next/link";
const pages: Record<string, { title: string; paragraphs: string[] }> = {
  "": {
    title: "开始使用 Reign",
    paragraphs: [
      "在电脑安装 Reign 原生客户端，打开后会得到一个六位设备编号。原生桌面端同时支持被控和控制；手机及浏览器用于连接电脑。",
      "在控制端输入设备编号。首次连接请在被控电脑确认授权，也可以在电脑设置访问密码。登录同步设备不会替代这一步授权。",
      "被控端显示已安装 Agent 及接入状态。Codex App 需要由 Reign 启动并连接到共同的 app-server；手动启动的 Codex App 请按状态面板的引导重新接入。Reign 退出不应终止本机的 Agent 任务。",
      "会话按工作目录汇总，最近更新的排在前面。支持实时消息、模型选择、图片与文件传输。",
    ],
  },
  "self-hosting": {
    title: "部署你自己的服务",
    paragraphs: [
      "开源 Worker 包含官网、Web 控制端、信令和加密中继，不需要账号或订阅。Cloudflare 的资源配额与计费仍按你自己的账号执行。",
      "从 GitHub 的 Deploy to Cloudflare 入口创建 Worker。D1 保存设备与配对元数据，Durable Object 管理设备房间与 WebSocket。初始化数据库后，将生成的 workers.dev HTTPS 根地址填入客户端即可。",
      "客户端只填写一个服务地址。公共 STUN 与自建 STUN 并行辅助 UDP 直连；可选 Go 探测器在 26330/TCP、26330/UDP 提供 TCP 与 UDP 探测。将探测域名设为 DNS only，再通过 SELF_STUN 和 TCP_STUN 发布入口。",
      "无需 TURN、DERP 或 cloudflared。无法直连时使用你自己的 Worker 中继；请留意 Cloudflare WebSocket、DO 资源账单。部署步骤和命令见仓库 docs/platform/self-hosting.md。",
    ],
  },
  privacy: {
    title: "数据与隐私",
    paragraphs: [
      "会话消息、工具输出和附件通过配对密钥端到端加密传输，服务端不保存会话明文或配对私钥。",
      "服务器保存设备公钥、名称、设备编号、配对关系和在线状态。官方商业版另保存登录资料、设备归属、套餐与账单信息。",
      "浏览器使用 IndexedDB 缓存设备密钥、会话和附件，仅在本浏览器保存。公共电脑使用后请在控制台清除本地数据，并在被控端撤销该设备授权。",
      "官方免费账号可同步设备并使用直连；中继和设备额度以账号权益页为准。自行部署的开源服务不执行商业套餐限制。",
    ],
  },
  architecture: {
    title: "连接与架构",
    paragraphs: [
      "所有客户端使用同一个服务根地址。/p2p/config 发布当前服务能力，/p2p/signal 仅传递握手与直连信令，/p2p/relay 传递加密业务帧。",
      "原生端并行尝试 WebRTC/UDP 和 TCP 打洞；浏览器使用 WebRTC。直连不可用时，仅有中继权益的连接才打开官方业务中继。",
      "官方 Worker 把开源连接核心与闭源身份、支付、权益策略组合在同一个部署中。账号配额通过独立 Durable Object 原子分配，D1 记录归属。被控端会校验短期签名授权，包括直连路径。",
    ],
  },
};
export default async function Docs({
  params,
}: {
  params: Promise<{ slug?: string[] }>;
}) {
  const { slug } = await params;
  const page = pages[(slug || []).join("/")] || {
    title: "页面不存在",
    paragraphs: ["请从左侧选择文档。"],
  };
  return (
    <main className="docs">
      <nav>
        <Link className="brand" href="/">
          ♜ Reign
        </Link>
        <Link href="/chat">打开控制台 ↗</Link>
      </nav>
      <div className="docs-layout">
        <aside>
          {Object.entries(pages).map(([slug, page]) => (
            <Link key={slug} href={"/docs/" + slug}>
              {page.title}
            </Link>
          ))}
        </aside>
        <article>
          <span className="eyebrow">REIGN DOCUMENTATION</span>
          <h1>{page.title}</h1>
          {page.paragraphs.map((p) => (
            <p key={p}>{p}</p>
          ))}
        </article>
      </div>
    </main>
  );
}
