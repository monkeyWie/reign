"use client";
import Link from "next/link";
import { useEffect, useState } from "react";
import { ArrowUpRight, Check, LoaderCircle, X } from "lucide-react";
interface Plan {
  id: string;
  name: string;
  priceLabel: string;
  purchasable: boolean;
  limits: {
    maxHostDevices: number | null;
    maxAgentsPerDevice: number | null;
    maxConcurrentConnections: number | null;
    allowRelay: boolean;
  };
}
interface PricingData {
  deployment: string;
  providers: string[];
  plans: Plan[];
}
export function PricingCards() {
  const [data, setData] = useState<PricingData>();
  const [error, setError] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setError(false);
    setData(undefined);
    fetch("/api/account", { credentials: "include", signal: controller.signal })
      .then(async (r) => {
        if (!r.ok) throw Error();
        return r.json();
      })
      .then((d: PricingData) => {
        if (!Array.isArray(d.plans) || !Array.isArray(d.providers))
          throw Error();
        setData(d);
      })
      .catch(() => {
        if (!controller.signal.aborted) setError(true);
      });
    return () => controller.abort();
  }, [attempt]);
  return (
    <>
      <div className="pricing-cards" aria-live="polite">
        {!data && !error && (
          <div className="pricing-loading">
            <LoaderCircle size={20} className="spin" />
            <p>正在读取当前服务的套餐与价格…</p>
          </div>
        )}
        {error && (
          <div className="pricing-loading">
            <h2>暂时无法加载官方套餐</h2>
            <p>价格与权益以服务端配置为准，请稍后重试。</p>
            <button className="button" onClick={() => setAttempt(attempt + 1)}>
              重新加载
            </button>
          </div>
        )}
        {data?.deployment === "self-hosted" && (
          <div className="pricing-selfhost-info">
            <span className="site-eyebrow">SELF-HOSTED INSTANCE</span>
            <h2>你正在访问自建服务。</h2>
            <p>
              这套开源部署不启用商业账号或套餐限制，可以直接连接自己的设备。
            </p>
            <Link className="button primary" href="/chat">
              打开控制台 <ArrowUpRight size={15} />
            </Link>
            <a className="site-text-link" href="https://myreign.cloud/pricing">
              了解官方服务 ↗
            </a>
          </div>
        )}
        {data?.deployment !== "self-hosted" &&
          data?.plans.map((plan) => (
            <article
              key={plan.id}
              className={
                "pricing-card " + (plan.id === "pro" ? "featured" : "")
              }
            >
              <div className="plan-heading">
                <h2>{plan.name}</h2>
                {plan.id === "pro" && (
                  <span className="site-tag">更多连接空间</span>
                )}
              </div>
              <p className="plan-description">
                {plan.id === "free"
                  ? "偶尔远程，先从直连开始。"
                  : "更多设备，以及需要时的官方中继。"}
              </p>
              <div className="plan-price">
                {plan.priceLabel || "价格待公布"}
              </div>
              <ul>
                <li>
                  <Check size={16} />
                  {plan.limits.maxHostDevices === null
                    ? "不限被控电脑"
                    : `${plan.limits.maxHostDevices} 台被控电脑`}
                </li>
                <li>
                  <Check size={16} />
                  {plan.limits.maxAgentsPerDevice === null
                    ? "每台电脑不限 Agent 产品"
                    : `每台最多 ${plan.limits.maxAgentsPerDevice} 种 Agent 产品`}
                </li>
                <li>
                  <Check size={16} />
                  支持范围内的会话管理
                </li>
                <li>
                  <Check size={16} />
                  设备目录同步 · P2P 直连
                </li>
                <li>
                  <Check size={16} />
                  会话与附件端到端加密
                </li>
                <li
                  className={!plan.limits.allowRelay ? "plan-unavailable" : ""}
                >
                  {plan.limits.allowRelay ? (
                    <Check size={16} />
                  ) : (
                    <X size={16} />
                  )}
                  官方加密中继{!plan.limits.allowRelay ? "不包含" : ""}
                </li>
                {plan.limits.maxConcurrentConnections !== null && (
                  <li>
                    <Check size={16} />
                    最多 {plan.limits.maxConcurrentConnections} 对并发设备连接
                  </li>
                )}
              </ul>
              {plan.id === "free" ? (
                data.providers.length ? (
                  <Link className="button" href="/account">
                    登录开始使用 <ArrowUpRight size={15} />
                  </Link>
                ) : (
                  <button className="button" disabled>
                    官方登录即将开放
                  </button>
                )
              ) : plan.purchasable ? (
                <Link className="button primary" href="/account">
                  查看订阅 <ArrowUpRight size={15} />
                </Link>
              ) : (
                <button className="button primary" disabled>
                  暂未开放购买
                </button>
              )}
              <small>
                {plan.id === "free"
                  ? "手机／浏览器不占被控电脑名额。"
                  : "生效范围与最终金额以订阅确认页为准。"}
              </small>
            </article>
          ))}
        <article className="pricing-card selfhost-plan">
          <span className="site-eyebrow">YOUR INFRASTRUCTURE</span>
          <h2>Self-hosted</h2>
          <p className="plan-description">自己部署，自己决定。</p>
          <div className="plan-price">开源免费</div>
          <ul>
            <li>
              <Check size={16} />
              不设商业设备额度
            </li>
            <li>
              <Check size={16} />
              不设商业 Agent 产品额度
            </li>
            <li>
              <Check size={16} />
              自己的 Web、信令与加密中继
            </li>
            <li>
              <Check size={16} />
              无需账号与订阅
            </li>
            <li>
              <Check size={16} />
              可选自建 TCP／UDP 探测
            </li>
            <li>
              <Check size={16} />
              MIT 许可，核心源码可修改
            </li>
          </ul>
          <Link className="button" href="/docs/self-hosting">
            开始部署 <ArrowUpRight size={15} />
          </Link>
          <small>Cloudflare 等基础设施费用由你承担。</small>
        </article>
      </div>
      {data?.deployment !== "self-hosted" && data?.plans.length === 0 && (
        <p className="pricing-status">
          此服务尚未公布可用套餐，请以账号页的开放状态为准。
        </p>
      )}
      <p className="pricing-status">
        官方套餐实时读取自当前服务。登录与支付能力按实际开放状态显示；Reign
        不包含 Agent 模型订阅或 API 用量费用。
      </p>
    </>
  );
}
