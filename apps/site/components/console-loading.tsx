import "./console-loading.css";

export function ConsoleLoading() {
  return (
    <div className="console-loading" role="status">
      <div className="console-loading-content">
        <div className="console-loading-emblem" aria-hidden="true">
          <span className="console-loading-orbit" />
          <span className="console-loading-tile">♜</span>
        </div>
        <div className="console-loading-wordmark">Reign</div>
        <p className="console-loading-caption">正在打开控制台</p>
        <div className="console-loading-track" aria-hidden="true">
          <span />
        </div>
      </div>
    </div>
  );
}
