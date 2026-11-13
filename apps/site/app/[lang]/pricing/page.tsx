import { pageMetadata, marketingMeta } from "../../../lib/seo";
import { translate, isLocale } from "../../../lib/i18n";
import { notFound } from "next/navigation";
import { LocalizedLink as Link } from "../../../components/locale";
import { PageIntro, SiteShell } from "../../../components/site";
import { PricingCards } from "../../../components/pricing";

export default async function Pricing({
  params,
}: {
  params: Promise<{ lang: string }>;
}) {
  const { lang } = await params;
  if (!isLocale(lang)) notFound();
  const t = translate(lang);
  const questions = [
    [
      t("Free 和 Pro 的区别是什么？"),
      t(
        "官方免费方案侧重设备同步与直连，付费方案可提供更多设备额度和官方中继。上方数值、价格和购买状态从当前服务实时读取；尚未开放的套餐不会接受购买。",
      ),
    ],
    [
      t("设备与 Agent 怎么计数？"),
      t(
        "额度按被控电脑计算，手机和浏览器控制端不占电脑名额。Codex App 与 CLI 合并为一个 Codex 产品。Agent 产品配额不代表所有 Provider 都已接入，当前支持范围见文档。",
      ),
    ],
    [
      t("免费直连失败了怎么办？"),
      t(
        "官方免费方案不提供官方业务中继，因此有些网络下可能无法连接。可以使用开放后的付费中继，或部署自己的开源 Worker，使用自己的加密中继。",
      ),
    ],
    [
      t("自建真的是免费的吗？"),
      t(
        "开源软件不收订阅费，也不执行官方商业配额；但 Cloudflare Worker、DO、D1 与探测服务器仍受你云账号的资源限额及计费影响。",
      ),
    ],
    [
      t("包含 Codex 或其他模型费用吗？"),
      t(
        "不包含。你需要自行安装 Agent、登录模型账号，并承担对应模型服务或 API 的费用。Reign 是连接和会话控制工具，不提供模型算力。",
      ),
    ],
    [
      t("订阅后会改变本地 Agent 的权限吗？"),
      t(
        "不会替你自动批准 Agent 的工具请求，也不会把本地 Agent 变成云托管。服务权益控制远程连接和资源额度；Agent 自己的权限与审批仍按原有规则处理。",
      ),
    ],
  ];
  return (
    <SiteShell>
      <main id="main">
        <PageIntro
          eyebrow="CHOOSE HOW YOU CONNECT"
          title={
            <>
              {t("从直连开始，")}
              <br />
              <span>{t("按需要扩展。")}</span>
            </>
          }
        >
          {t(
            "用官方服务省去部署，也可以拥有自己的连接服务。你使用的 Agent、模型账号与本地项目始终由你管理。",
          )}
        </PageIntro>
        <section className="site-width pricing-section">
          <PricingCards />
        </section>
        <section className="site-width pricing-notes">
          <div>
            <h2>{t("不变的是你的数据边界。")}</h2>
            <p>
              {t(
                "无论官方还是自建，会话与附件都使用端到端加密。中继只改变网络路径，不改变谁能读取你的会话。",
              )}
            </p>
            <Link className="site-text-link" href="/docs/privacy">
              {t("查看安全与隐私 →")}
            </Link>
          </div>
          <div>
            <h2>{t("先确认 Agent 接入范围。")}</h2>
            <p>
              {t(
                "目前提供 Codex App 和 CLI Provider。套餐中的 Agent 数量是产品额度，新的 Provider 接入后才可实际使用。",
              )}
            </p>
            <Link className="site-text-link" href="/docs/agents">
              {t("查看支持矩阵 →")}
            </Link>
          </div>
        </section>
        <section className="site-section site-width faq-section">
          <span className="site-eyebrow">COMMON QUESTIONS</span>
          <h2>{t("选择之前，你可能想了解。")}</h2>
          {questions.map(([q, a]) => (
            <details key={q}>
              <summary>
                {q}
                <span>+</span>
              </summary>
              <p>{a}</p>
            </details>
          ))}
        </section>
      </main>
    </SiteShell>
  );
}

export async function generateMetadata({
  params,
}: {
  params: Promise<{ lang: string }>;
}) {
  const { lang } = await params;
  if (!isLocale(lang)) notFound();
  const [title, description] = marketingMeta["/pricing"][lang];
  return pageMetadata(lang, "/pricing", title, description);
}
