import type { Metadata } from "next";
import Link from "next/link";
import {
  ArrowRight,
  FileImage,
  FolderOpen,
  KeyRound,
  MessagesSquare,
  MonitorSmartphone,
  Network,
  ShieldCheck,
  WifiOff,
} from "lucide-react";
import {
  BottomCTA,
  PageIntro,
  SectionHeading,
  SiteShell,
} from "../../components/site";
import { ProductPreview } from "../../components/product-preview";
export const metadata: Metadata = {
  title: "特性 · Reign",
  description:
    "跨设备管理 Agent 会话，原生与 Web、直连优先、端到端加密及自行部署。",
};
export default function Features() {
  return (
    <SiteShell>
      <main id="main">
        <PageIntro
          eyebrow="THE WORKSPACE, EVERYWHERE"
          title={
            <>
              熟悉的 Agent，
              <br />
              <span>更自由的工作方式。</span>
            </>
          }
        >
          把电脑上的工作带到手边，保留原来的 Agent、模型与项目。
        </PageIntro>
        <div className="features-showcase site-width">
          <ProductPreview />
          <div>
            <span className="site-tag available">桌面 · 手机 · Web</span>
            <h2>
              一台电脑工作，
              <br />
              不止一台设备参与。
            </h2>
            <p>
              桌面既是被控端，也是控制端。手机和浏览器连接你的电脑；多台电脑，各自的项目和会话都能在一个界面找到。
            </p>
            <Link className="site-text-link" href="/docs/guides/devices">
              设备与配对 <ArrowRight size={14} />
            </Link>
          </div>
        </div>
        <section className="site-section site-width">
          <div className="feature-detail-grid">
            {[
              [
                FolderOpen,
                "以项目为中心",
                "会话按工作目录聚合，通过图标区分 Provider，按最近更新排序。Codex App 与 CLI 的可控路由独立处理。",
                "/docs/guides/sessions",
              ],
              [
                MessagesSquare,
                "对话顺着思路发生",
                "增量输出、Markdown、生成中的折叠活动、模型选择，以及可跨段落选择和复制的消息。",
                "/docs/guides/sessions",
              ],
              [
                FileImage,
                "图片与文件一起带上",
                "选择、拖放或在支持的环境粘贴附件；图片缩略图、放大切换和文件下载，附件加密传输。",
                "/docs/guides/files",
              ],
              [
                Network,
                "能直连，就直连",
                "原生端尝试 WebRTC／UDP 与 TCP，Web 使用标准 WebRTC。回退中继取决于自建服务或官方权益。",
                "/docs/guides/connections",
              ],
              [
                KeyRound,
                "每次连接，有明确的信任",
                "设备编号负责发现，首次访问由电脑确认或密码验证；已授权设备可以撤销。",
                "/docs/guides/devices",
              ],
              [
                WifiOff,
                "断线有缓存，重连有同步",
                "SQLite 与 IndexedDB 保存已同步内容。恢复连接后补最新快照，再继续增量订阅。",
                "/docs/guides/sessions",
              ],
              [
                ShieldCheck,
                "数据的边界，清清楚楚",
                "会话和附件端到端加密，密钥保存在设备上；云端只保存连接所需的元数据和可选账号信息。",
                "/docs/privacy",
              ],
              [
                MonitorSmartphone,
                "用自己的服务也一样",
                "自建 Worker 同时提供 Web、信令与加密中继，无商业设备或 Agent 配额。",
                "/docs/self-hosting",
              ],
            ].map(([Icon, title, description, url]) => {
              const I = Icon as typeof FolderOpen;
              return (
                <article key={String(title)}>
                  <I size={22} />
                  <h2>{String(title)}</h2>
                  <p>{String(description)}</p>
                  <Link href={String(url)} className="site-text-link">
                    阅读文档 <ArrowRight size={14} />
                  </Link>
                </article>
              );
            })}
          </div>
        </section>
        <section className="site-width support-section">
          <SectionHeading
            eyebrow="HONEST ABOUT SUPPORT"
            title="支持到哪里，就说到哪里。"
          >
            当前接入 Codex App 和 Codex CLI。多 Provider 架构已准备好，新的
            Agent 需要实现并验证适配后才会上线。
          </SectionHeading>
          <div className="support-grid">
            <div>
              <strong>Codex App</strong>
              <span className="site-tag available">已实现</span>
              <p>
                通过 Reign 启动并连接共享
                app-server。手动独立启动时可能需要重新接入。
              </p>
            </div>
            <div>
              <strong>Codex CLI</strong>
              <span className="site-tag available">已实现</span>
              <p>
                连接兼容后端。不同 app-server
                持有的运行中会话，不保证能直接接管。
              </p>
            </div>
            <div>
              <strong>Claude Code · Pi · ZCode</strong>
              <span className="site-tag">尚未接入</span>
              <p>欢迎贡献 Provider。Codex VS Code 扩展当前也不在支持范围内。</p>
            </div>
          </div>
          <Link className="site-text-link" href="/docs/agents">
            完整支持矩阵与前置条件 <ArrowRight size={15} />
          </Link>
        </section>
        <BottomCTA />
      </main>
    </SiteShell>
  );
}
