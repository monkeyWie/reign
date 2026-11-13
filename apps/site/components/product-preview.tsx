"use client";
import { useT } from "./locale";
import {
  ArrowUp,
  Check,
  ChevronDown,
  Folder,
  Plus,
  Terminal,
} from "lucide-react";

export function ProductPreview() {
  const t = useT();
  return (
    <div className="product-scene" aria-label={t("Reign 会话界面示意")}>
      <div className="product-window">
        <div className="window-top">
          <div className="window-dots">
            <i />
            <i />
            <i />
          </div>
          <span>Reign</span>
          <span className="preview-caption">{t("界面示意")}</span>
        </div>
        <div className="preview-workspace">
          <aside className="preview-sidebar">
            <span className="preview-label">{t("已连接设备")}</span>
            <div className="preview-device">
              <span className="online-dot" />
              <div>
                <strong>MacBook Pro</strong>
                <small>{t("P2P 直连")}</small>
              </div>
              <ChevronDown size={13} />
            </div>
            <span className="preview-label">{t("项目")}</span>
            <div className="preview-project">
              <Folder size={13} /> reign <ChevronDown size={12} />
            </div>
            <div className="preview-session active">
              <Terminal size={13} />
              {t("完善项目文档")}
            </div>
            <div className="preview-session">
              <Terminal size={13} />
              {t("检查连接状态")}
            </div>
            <span className="preview-label preview-bottom">
              {t("所有工作，井然有序。")}
            </span>
          </aside>
          <div className="preview-chat">
            <div className="preview-chat-header">
              <strong>{t("完善项目文档")}</strong>
              <span className="preview-direct">
                <span className="online-dot" /> P2P
              </span>
            </div>
            <div className="preview-messages">
              <div className="preview-user">
                {t("帮我梳理一下自建部署步骤。")}
              </div>
              <div className="preview-agent">
                <span className="preview-agent-logo">♜</span>
                <div>
                  <p>{t("已经整理好了，分为三个步骤：")}</p>
                  <ol>
                    <li>{t("部署你的 Cloudflare Worker")}</li>
                    <li>{t("在客户端填写服务地址")}</li>
                    <li>{t("配对设备，开始连接")}</li>
                  </ol>
                  <div className="preview-complete">
                    <Check size={12} />
                    {t("文档已更新")}
                  </div>
                </div>
              </div>
            </div>
            <div className="preview-composer">
              <span>{t("继续你的工作…")}</span>
              <div>
                <Plus size={15} />
                <small>
                  Codex <ChevronDown size={10} />
                </small>
                <span className="preview-send">
                  <ArrowUp size={13} />
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
      <div className="preview-note">
        <span className="online-dot" />
        {t("Agent 运行在你的电脑上")} <span>{t("端到端加密")}</span>
      </div>
    </div>
  );
}
