import React, { useState, useEffect } from 'react';
import { 
  Power, 
  Activity, 
  Settings, 
  Share2, 
  Shield, 
  ArrowUp, 
  ArrowDown, 
  Wifi, 
  CheckCircle2, 
  AlertCircle, 
  Clock, 
  Zap,
  Server
} from 'lucide-react';
import { StartTunnel, StopTunnel, GetMetrics, GetDefaultConfig, CheckCallHealth } from '../wailsjs/go/main/App';
import { core } from '../wailsjs/go/models';
import './App.css';

type EngineConfig = core.EngineConfig;
type EngineMetrics = core.EngineMetrics;

interface PipelineStep {
  phase: string;
  status: 'PENDING' | 'RUNNING' | 'OK' | 'FAILED';
  message?: string;
}

const DEFAULT_PIPELINE: PipelineStep[] = [
  { phase: 'DNS', status: 'PENDING' },
  { phase: 'VK_API', status: 'PENDING' },
  { phase: 'CAPTCHA', status: 'PENDING' },
  { phase: 'WRAP_AEAD', status: 'PENDING' },
  { phase: 'TURN_RELAY', status: 'PENDING' },
  { phase: 'WORKERS', status: 'PENDING' },
  { phase: 'TUN_ADAPTER', status: 'PENDING' },
  { phase: 'ACTIVE', status: 'PENDING' },
];

export default function App() {
  const [activeTab, setActiveTab] = useState<'dashboard' | 'settings' | 'bridge'>('dashboard');
  const [connectionState, setConnectionState] = useState<string>('DISCONNECTED');
  const [metrics, setMetrics] = useState<EngineMetrics>({
    state: 'DISCONNECTED',
    assigned_ip: '',
    upload_bytes: 0,
    download_bytes: 0,
    upload_rate_bps: 0,
    download_rate_bps: 0,
    active_workers: 0,
    total_workers: 9,
    latency_ms: 0,
    uptime_seconds: 0,
  });

  const [pipeline, setPipeline] = useState<PipelineStep[]>(DEFAULT_PIPELINE);
  const [config, setConfig] = useState<EngineConfig>({
    peer: '198.51.100.1:56003',
    password: 'secure_password_123',
    device_id: 'linux-desktop',
    vk_links: ['https://vk.com/call/join/vPT-ovf0Q_lkaKXi2vaRJK1JJgaOwBYjelhuAMQll1s'],
    workers: 9,
    obfs: 'audio',
    captcha_mode: 'auto',
    dns: '77.88.8.8',
    mtu: 1280,
    tun_name: 'netturn0',
    lan_bridge_enabled: false,
    lan_bridge_port: 24066,
    turn_tcp: false,
  });

  const [checkResult, setCheckResult] = useState<string>('');
  const [isChecking, setIsChecking] = useState<boolean>(false);

  useEffect(() => {
    // Загрузка начальной конфигурации из Go
    GetDefaultConfig().then((cfg) => {
      if (cfg && cfg.peer) {
        setConfig(cfg);
      }
    }).catch(() => {});

    // Подписка на события Wails Runtime
    if ((window as any).runtime) {
      (window as any).runtime.EventsOn('state_change', (state: string) => {
        setConnectionState(state);
        if (state === 'DISCONNECTED') {
          setPipeline(DEFAULT_PIPELINE);
        }
      });

      (window as any).runtime.EventsOn('metrics_update', (data: EngineMetrics) => {
        setMetrics(data);
      });

      (window as any).runtime.EventsOn('pipeline_step', (ev: any) => {
        setPipeline((prev) =>
          prev.map((step) =>
            step.phase === ev.phase
              ? { ...step, status: ev.status, message: ev.message }
              : step
          )
        );
      });
    }

    const interval = setInterval(() => {
      GetMetrics().then(setMetrics).catch(() => {});
    }, 1000);

    return () => clearInterval(interval);
  }, []);

  const handleToggleConnect = async () => {
    if (connectionState === 'CONNECTED' || connectionState === 'CONNECTING') {
      await StopTunnel();
      setConnectionState('DISCONNECTED');
      setPipeline(DEFAULT_PIPELINE);
    } else {
      setConnectionState('CONNECTING');
      try {
        await StartTunnel(config);
      } catch (err: any) {
        setConnectionState('ERROR');
      }
    }
  };

  const handleCheckLink = async () => {
    if (!config.vk_links[0]) return;
    setIsChecking(true);
    setCheckResult('Проверка доступности релея...');
    try {
      const res = await CheckCallHealth(config.vk_links[0]);
      setCheckResult(res);
    } catch (e: any) {
      setCheckResult(`Ошибка проверки: ${e?.message || e}`);
    } finally {
      setIsChecking(false);
    }
  };

  const formatRate = (bps: number) => {
    const bits = bps * 8;
    if (bits >= 1000000) return `${(bits / 1000000).toFixed(1)} Mbps`;
    if (bits >= 1000) return `${(bits / 1000).toFixed(1)} Kbps`;
    return `${bits} bps`;
  };

  const formatBytes = (bytes: number) => {
    if (bytes >= 1073741824) return `${(bytes / 1073741824).toFixed(2)} GB`;
    if (bytes >= 1048576) return `${(bytes / 1048576).toFixed(1)} MB`;
    return `${(bytes / 1024).toFixed(0)} KB`;
  };

  const formatUptime = (seconds: number) => {
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = seconds % 60;
    return `${h.toString().padStart(2, '0')}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  };

  return (
    <div className="app-layout">
      {/* Sidebar */}
      <aside className="sidebar">
        <div className="logo-area">
          <div className="logo-icon">
            <Zap size={22} color="#040812" />
          </div>
          <div className="logo-text">
            <h1>NetTurn</h1>
            <span>VK Calls Relay</span>
          </div>
        </div>

        <nav className="nav-menu">
          <button 
            className={`nav-item ${activeTab === 'dashboard' ? 'active' : ''}`}
            onClick={() => setActiveTab('dashboard')}
          >
            <Activity size={18} />
            Туннель
          </button>
          <button 
            className={`nav-item ${activeTab === 'settings' ? 'active' : ''}`}
            onClick={() => setActiveTab('settings')}
          >
            <Settings size={18} />
            Настройки
          </button>
          <button 
            className={`nav-item ${activeTab === 'bridge' ? 'active' : ''}`}
            onClick={() => setActiveTab('bridge')}
          >
            <Share2 size={18} />
            LAN Мост
          </button>
        </nav>

        <div className="sidebar-status">
          <div className={`status-dot ${
            connectionState === 'CONNECTED' ? 'active' : 
            connectionState === 'CONNECTING' ? 'connecting' : 
            connectionState === 'ERROR' ? 'error' : 'idle'
          }`} />
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <span style={{ fontSize: '0.8rem', fontWeight: 700 }}>
              {connectionState === 'CONNECTED' ? 'Защищено' : 
               connectionState === 'CONNECTING' ? 'Подключение...' : 
               connectionState === 'ERROR' ? 'Ошибка' : 'Отключено'}
            </span>
            <span style={{ fontSize: '0.7rem', color: 'var(--text-muted)' }}>
              {connectionState === 'CONNECTED' ? `IP: ${metrics.assigned_ip || '10.70.x.y'}` : 'Трафик не туннелируется'}
            </span>
          </div>
        </div>
      </aside>

      {/* Main Content Area */}
      <main className="content-area">
        {activeTab === 'dashboard' && (
          <>
            {/* Hero Connection Card */}
            <div className="hero-card">
              <button 
                className={`power-btn ${
                  connectionState === 'CONNECTED' ? 'connected' : 
                  connectionState === 'CONNECTING' ? 'connecting' : ''
                }`}
                onClick={handleToggleConnect}
              >
                <Power 
                  size={46} 
                  color={
                    connectionState === 'CONNECTED' ? 'var(--accent-emerald)' : 
                    connectionState === 'CONNECTING' ? 'var(--accent-amber)' : 'var(--text-secondary)'
                  } 
                />
              </button>
              
              <h2 style={{ fontSize: '1.25rem', fontWeight: 800, marginBottom: '6px' }}>
                {connectionState === 'CONNECTED' ? 'Туннель активен' : 
                 connectionState === 'CONNECTING' ? 'Установка соединения...' : 'Нажмите для подключения'}
              </h2>
              <p style={{ fontSize: '0.82rem', color: 'var(--text-secondary)' }}>
                {connectionState === 'CONNECTED' ? 'Все пакеты маскируются под медиа-трафик VK Calls' : 'Прямое подключение к серверу через релеи OKCDN'}
              </p>

              {/* Connection Pipeline */}
              <div className="pipeline-widget">
                <div className="pipeline-header">
                  <span>Интерактивный конвейер (Pipeline)</span>
                  <span>{connectionState}</span>
                </div>
                <div className="pipeline-track">
                  {pipeline.map((step) => (
                    <div key={step.phase} className="pipeline-step">
                      <div className={`step-circle ${
                        step.status === 'OK' ? 'ok' : 
                        step.status === 'RUNNING' ? 'running' : 
                        step.status === 'FAILED' ? 'failed' : ''
                      }`}>
                        {step.status === 'OK' ? <CheckCircle2 size={16} /> : 
                         step.status === 'FAILED' ? <AlertCircle size={16} /> : 
                         step.phase.substring(0, 2)}
                      </div>
                      <span className={`step-label ${step.status !== 'PENDING' ? 'active' : ''}`}>
                        {step.phase}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            </div>

            {/* Metrics Dashboard */}
            <div className="metrics-grid">
              <div className="metric-card">
                <div className="metric-title">
                  <ArrowDown size={14} color="var(--accent-emerald)" />
                  Входящая скорость
                </div>
                <div className="metric-value" style={{ color: 'var(--accent-emerald)' }}>
                  {formatRate(metrics.download_rate_bps)}
                </div>
                <div className="metric-sub">
                  Всего: {formatBytes(metrics.download_bytes)}
                </div>
              </div>

              <div className="metric-card">
                <div className="metric-title">
                  <ArrowUp size={14} color="var(--accent-cyan)" />
                  Исходящая скорость
                </div>
                <div className="metric-value" style={{ color: 'var(--accent-cyan)' }}>
                  {formatRate(metrics.upload_rate_bps)}
                </div>
                <div className="metric-sub">
                  Всего: {formatBytes(metrics.upload_bytes)}
                </div>
              </div>

              <div className="metric-card">
                <div className="metric-title">
                  <Wifi size={14} color="var(--accent-amber)" />
                  Активные воркеры
                </div>
                <div className="metric-value">
                  {metrics.active_workers} / {metrics.total_workers}
                </div>
                <div className="metric-sub">
                  Группа потоков OKCDN
                </div>
              </div>

              <div className="metric-card">
                <div className="metric-title">
                  <Clock size={14} color="var(--accent-violet)" />
                  Время сессии
                </div>
                <div className="metric-value">
                  {formatUptime(metrics.uptime_seconds)}
                </div>
                <div className="metric-sub">
                  Адаптивный страж: Активен
                </div>
              </div>
            </div>
          </>
        )}

        {activeTab === 'settings' && (
          <div className="settings-container">
            <div className="settings-card">
              <h3 style={{ fontSize: '1.05rem', fontWeight: 700, marginBottom: '16px' }}>
                Параметры подключения к серверу
              </h3>

              <div className="form-group">
                <label>Адрес VPS сервера (IP:Port)</label>
                <input 
                  className="form-input" 
                  value={config.peer} 
                  onChange={(e) => setConfig({ ...config, peer: e.target.value })}
                  placeholder="198.51.100.1:56003"
                />
              </div>

              <div className="form-group">
                <label>Пароль авторизации / Ключ WRAP</label>
                <input 
                  type="password"
                  className="form-input" 
                  value={config.password} 
                  onChange={(e) => setConfig({ ...config, password: e.target.value })}
                  placeholder="••••••••••••••••"
                />
              </div>

              <div className="form-group">
                <label>Ссылка на звонок VK Calls</label>
                <div style={{ display: 'flex', gap: '10px' }}>
                  <input 
                    className="form-input" 
                    style={{ flex: 1 }}
                    value={config.vk_links[0] || ''} 
                    onChange={(e) => setConfig({ ...config, vk_links: [e.target.value] })}
                    placeholder="https://vk.com/call/join/..."
                  />
                  <button 
                    className="btn-secondary" 
                    onClick={handleCheckLink}
                    disabled={isChecking}
                  >
                    <Zap size={16} />
                    {isChecking ? 'Проверка...' : 'Проверить'}
                  </button>
                </div>
                {checkResult && (
                  <span style={{ fontSize: '0.78rem', color: checkResult.includes('Ошибка') ? 'var(--accent-rose)' : 'var(--accent-emerald)', marginTop: '4px' }}>
                    {checkResult}
                  </span>
                )}
              </div>

              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px' }}>
                <div className="form-group">
                  <label>Количество воркеров (потоков)</label>
                  <select 
                    className="form-input"
                    value={config.workers}
                    onChange={(e) => setConfig({ ...config, workers: parseInt(e.target.value) })}
                  >
                    <option value={9}>9 воркеров (Рекомендуется)</option>
                    <option value={18}>18 воркеров (Высокая скорость)</option>
                    <option value={27}>27 воркеров (Экстремальный)</option>
                  </select>
                </div>

                <div className="form-group">
                  <label>Режим обфускации RTP</label>
                  <select 
                    className="form-input"
                    value={config.obfs}
                    onChange={(e) => setConfig({ ...config, obfs: e.target.value })}
                  >
                    <option value="audio">Opus Audio (PT 111)</option>
                    <option value="video">VP8 Video (PT 96)</option>
                  </select>
                </div>
              </div>
            </div>
          </div>
        )}

        {activeTab === 'bridge' && (
          <div className="settings-container">
            <div className="settings-card">
              <h3 style={{ fontSize: '1.05rem', fontWeight: 700, marginBottom: '8px' }}>
                Dual LAN Bridge (PCVPN)
              </h3>
              <p style={{ fontSize: '0.82rem', color: 'var(--text-secondary)', marginBottom: '20px' }}>
                Раздача туннелированного интернета на смартфоны, Smart TV и консоли в домашней сети через единый порт.
              </p>

              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '16px', background: 'var(--bg-card)', borderRadius: '8px', marginBottom: '20px' }}>
                <div>
                  <div style={{ fontWeight: 600, fontSize: '0.9rem' }}>Автоопределение SOCKS5 + HTTP CONNECT</div>
                  <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>Порт 24066 слушает оба протокола одновременно</div>
                </div>
                <span style={{ padding: '6px 12px', background: 'rgba(16, 185, 129, 0.15)', color: 'var(--accent-emerald)', borderRadius: '6px', fontSize: '0.8rem', fontWeight: 700 }}>
                  АКТИВЕН
                </span>
              </div>

              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px' }}>
                <div style={{ padding: '14px', background: 'var(--bg-card)', borderRadius: '8px' }}>
                  <div style={{ fontSize: '0.78rem', color: 'var(--text-muted)', marginBottom: '4px' }}>Адрес в локальной сети:</div>
                  <div className="font-mono" style={{ fontSize: '0.95rem', fontWeight: 700, color: 'var(--accent-cyan)' }}>
                    http://netturn.local:24066
                  </div>
                </div>

                <div style={{ padding: '14px', background: 'var(--bg-card)', borderRadius: '8px' }}>
                  <div style={{ fontSize: '0.78rem', color: 'var(--text-muted)', marginBottom: '4px' }}>SOCKS5 адрес:</div>
                  <div className="font-mono" style={{ fontSize: '0.95rem', fontWeight: 700, color: 'var(--accent-cyan)' }}>
                    socks5://netturn.local:24066
                  </div>
                </div>
              </div>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}
