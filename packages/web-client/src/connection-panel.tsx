"use client";
import { useEffect, useRef } from "react";
import { Check, Circle, LoaderCircle, Minus, RefreshCw } from "lucide-react";
import type { ConnectionProgress } from "@reignhub/client-sdk";
import { Button, Dialog } from "./ui";
export interface ConnectionAttempt {
  name: string;
  service: string;
  code: string;
  started: number;
  finished?: number;
  active: boolean;
  error?: string;
  path?: string;
  steps: ConnectionProgress[];
}
export function ConnectionPanel({
  attempt,
  open,
  onClose,
  onRetry,
  onCancel,
}: {
  attempt?: ConnectionAttempt;
  open: boolean;
  onClose: () => void;
  onRetry: () => void;
  onCancel: () => void;
}) {
  const active = !!attempt?.active;
  const unavailable =
    !attempt?.path ||
    ["Disconnected", "尚未连接", "已取消"].includes(attempt.path);
  const timeline = useRef<HTMLOListElement>(null);
  const follow = useRef(true);
  useEffect(() => {
    follow.current = true;
  }, [attempt?.started]);
  useEffect(() => {
    if (follow.current && timeline.current)
      timeline.current.scrollTop = timeline.current.scrollHeight;
  }, [attempt?.steps, open]);
  return (
    <Dialog
      title="连接"
      open={open}
      onOpenChange={(value) => {
        if (!value) onClose();
      }}
    >
      <div className="connection-panel">
        <div className="connection-target">
          <strong>{attempt?.name || `设备 ${attempt?.code || ""}`}</strong>
          <small>{attempt?.service}</small>
        </div>
        <div className="connection-summary" role="status">
          {active ? (
            <LoaderCircle size={16} className="spin" />
          ) : attempt?.error || unavailable ? (
            <Circle size={14} />
          ) : (
            <Check size={16} />
          )}
          <span>
            {active
              ? "正在连接"
              : attempt?.error
                ? "连接未完成"
                : attempt?.path || "尚未连接"}
          </span>
          {attempt?.finished && (
            <small>
              {((attempt.finished - attempt.started) / 1000).toFixed(1)}s
            </small>
          )}
        </div>
        {!!attempt?.steps.length && (
          <ol
            className="connection-timeline"
            aria-label="连接步骤"
            ref={(el) => {
              timeline.current = el;
              if (el && follow.current) el.scrollTop = el.scrollHeight;
            }}
            onScroll={(e) => {
              const el = e.currentTarget;
              follow.current =
                el.scrollHeight - el.scrollTop - el.clientHeight < 36;
            }}
          >
            {attempt.steps.map((step) => (
              <li key={step.id} data-state={step.state}>
                {step.state === "success" ? (
                  <Check size={12} />
                ) : step.state === "warning" ? (
                  <Minus size={12} />
                ) : (
                  <Circle size={10} />
                )}
                <div>
                  <span>{step.message}</span>
                  {step.detail && <small>{step.detail}</small>}
                </div>
                <time>
                  {new Date(step.at).toLocaleTimeString([], { hour12: false })}
                </time>
              </li>
            ))}
          </ol>
        )}
        {attempt?.error && (
          <p className="connection-failure">{attempt.error}</p>
        )}
        <div className="connection-panel-actions">
          <Button onClick={onClose}>关闭</Button>
          {active ? (
            <Button onClick={onCancel}>取消连接</Button>
          ) : (
            <Button variant="primary" onClick={onRetry}>
              <RefreshCw size={14} />
              重试
            </Button>
          )}
        </div>
      </div>
    </Dialog>
  );
}
