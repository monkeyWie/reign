import "./console-loading.css";

export function ConsoleLoading() {
  return (
    <div className="console-loading" role="status">
      <div className="console-loading-content">
        <div className="console-loading-emblem" aria-hidden="true">
          <span className="console-loading-piece" />
          <span className="console-loading-piece" />
          <span className="console-loading-piece" />
          <span className="console-loading-piece" />
          <span className="console-loading-piece" />
          <span className="console-loading-piece" />
        </div>
        <div className="console-loading-wordmark">Reign</div>
        <p className="console-loading-caption">Opening your workspace</p>
      </div>
    </div>
  );
}
