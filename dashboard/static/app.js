// LLMTrace Dashboard — Frontend Application

(function() {
    'use strict';

    // --- Color Palette ---
    const COLORS = {
        blue: '#38bdf8',
        green: '#10b981',
        yellow: '#f59e0b',
        red: '#ef4444',
        purple: '#a78bfa',
        pink: '#f472b6',
        cyan: '#22d3ee',
        orange: '#fb923c',
    };

    const PROVIDER_COLORS = [COLORS.blue, COLORS.green, COLORS.yellow, COLORS.purple, COLORS.pink, COLORS.cyan, COLORS.orange, COLORS.red];

    const CHART_DEFAULTS = {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
            legend: {
                labels: { color: '#94a3b8', font: { size: 11 } },
            },
        },
        scales: {
            x: {
                ticks: { color: '#64748b', font: { size: 10 } },
                grid: { color: 'rgba(51, 65, 85, 0.5)' },
            },
            y: {
                ticks: { color: '#64748b', font: { size: 10 } },
                grid: { color: 'rgba(51, 65, 85, 0.5)' },
            },
        },
    };

    // --- Chart instances ---
    const charts = {};

    // --- API helpers ---
    async function fetchJSON(url) {
        const resp = await fetch(url);
        if (!resp.ok) throw new Error('HTTP ' + resp.status);
        return resp.json();
    }

    function formatNumber(n) {
        if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
        if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K';
        return n.toString();
    }

    function formatCost(v) {
        return '$' + v.toFixed(4);
    }

    function formatMs(v) {
        return v.toFixed(1);
    }

    // --- Navigation ---
    const navLinks = document.querySelectorAll('.nav-link');
    const pages = document.querySelectorAll('.page');

    navLinks.forEach(function(link) {
        link.addEventListener('click', function(e) {
            e.preventDefault();
            var page = this.dataset.page;
            navLinks.forEach(function(l) { l.classList.remove('active'); });
            pages.forEach(function(p) { p.classList.remove('active'); });
            this.classList.add('active');
            document.getElementById('page-' + page).classList.add('active');
            loadPage(page);
        });
    });

    function loadPage(page) {
        switch (page) {
            case 'overview': refreshOverview(); break;
            case 'providers': refreshProviders(); break;
            case 'models': refreshModels(); break;
            case 'costs': refreshCosts(); break;
            case 'errors': refreshErrors(); break;
            case 'traces': refreshTraces(); break;
        }
    }

    // --- Chart helpers ---
    function getOrCreateChart(id, config) {
        if (charts[id]) {
            charts[id].destroy();
        }
        var ctx = document.getElementById(id);
        if (!ctx) return null;
        charts[id] = new Chart(ctx, config);
        return charts[id];
    }

    // --- Overview ---
    async function refreshOverview() {
        try {
            var data = await fetchJSON('/api/overview');
            document.getElementById('stat-requests').textContent = formatNumber(data.total_requests);
            document.getElementById('stat-tokens').textContent = formatNumber(data.total_tokens);
            document.getElementById('stat-input-tokens').textContent = formatNumber(data.input_tokens);
            document.getElementById('stat-output-tokens').textContent = formatNumber(data.output_tokens);
            document.getElementById('stat-cost').textContent = formatCost(data.total_cost_usd);
            document.getElementById('stat-active').textContent = data.active_requests;
            document.getElementById('stat-errors').textContent = data.total_errors;
            document.getElementById('stat-latency').textContent = formatMs(data.avg_latency_ms);
            document.getElementById('stat-providers').textContent = data.provider_count;
            document.getElementById('stat-models').textContent = data.model_count;
            document.getElementById('overview-timestamp').textContent = 'Updated: ' + new Date(data.timestamp).toLocaleTimeString();
        } catch (e) {
            console.error('overview fetch failed:', e);
        }

        // Provider charts
        try {
            var provData = await fetchJSON('/api/providers');
            var providers = provData.providers || [];

            getOrCreateChart('chart-provider-requests', {
                type: 'bar',
                data: {
                    labels: providers.map(function(p) { return p.name; }),
                    datasets: [{
                        label: 'Requests',
                        data: providers.map(function(p) { return p.requests; }),
                        backgroundColor: PROVIDER_COLORS.slice(0, providers.length),
                        borderRadius: 4,
                    }],
                },
                options: Object.assign({}, CHART_DEFAULTS, { plugins: { legend: { display: false } } }),
            });

            getOrCreateChart('chart-provider-cost', {
                type: 'doughnut',
                data: {
                    labels: providers.map(function(p) { return p.name; }),
                    datasets: [{
                        data: providers.map(function(p) { return p.cost_usd; }),
                        backgroundColor: PROVIDER_COLORS.slice(0, providers.length),
                        borderWidth: 0,
                    }],
                },
                options: {
                    responsive: true,
                    maintainAspectRatio: false,
                    plugins: { legend: { labels: { color: '#94a3b8', font: { size: 11 } }, position: 'right' } },
                },
            });
        } catch (e) {
            console.error('provider charts failed:', e);
        }

        // Latency chart
        try {
            var latData = await fetchJSON('/api/latency');
            var latProviders = latData.providers || [];
            if (latProviders.length > 0) {
                var datasets = latProviders.slice(0, 6).map(function(p, i) {
                    var bucketData = (p.buckets || []).map(function(b) { return { x: b.upper_ms, y: b.count }; });
                    return {
                        label: p.provider + '/' + p.model,
                        data: bucketData,
                        borderColor: PROVIDER_COLORS[i % PROVIDER_COLORS.length],
                        backgroundColor: 'transparent',
                        tension: 0.3,
                        pointRadius: 2,
                    };
                });
                getOrCreateChart('chart-latency', {
                    type: 'line',
                    data: { datasets: datasets },
                    options: Object.assign({}, CHART_DEFAULTS, {
                        scales: {
                            x: Object.assign({}, CHART_DEFAULTS.scales.x, { type: 'linear', title: { display: true, text: 'Latency (ms)', color: '#64748b' } }),
                            y: Object.assign({}, CHART_DEFAULTS.scales.y, { title: { display: true, text: 'Count', color: '#64748b' } }),
                        },
                    }),
                });
            }
        } catch (e) {
            console.error('latency chart failed:', e);
        }

        // Error chart
        try {
            var errData = await fetchJSON('/api/errors');
            var byType = errData.by_type || [];
            if (byType.length > 0) {
                getOrCreateChart('chart-errors', {
                    type: 'doughnut',
                    data: {
                        labels: byType.map(function(e) { return e.type; }),
                        datasets: [{
                            data: byType.map(function(e) { return e.count; }),
                            backgroundColor: [COLORS.red, COLORS.orange, COLORS.yellow, COLORS.purple, COLORS.pink],
                            borderWidth: 0,
                        }],
                    },
                    options: {
                        responsive: true,
                        maintainAspectRatio: false,
                        plugins: { legend: { labels: { color: '#94a3b8', font: { size: 11 } }, position: 'right' } },
                    },
                });
            }
        } catch (e) {
            console.error('error chart failed:', e);
        }
    }

    // --- Providers ---
    window.refreshProviders = async function() {
        // Fetch basic provider data
        try {
            var data = await fetchJSON('/api/providers');
            var providers = data.providers || [];

            // Comparison bar chart
            getOrCreateChart('chart-provider-comparison', {
                type: 'bar',
                data: {
                    labels: providers.map(function(p) { return p.name; }),
                    datasets: [
                        { label: 'Requests', data: providers.map(function(p) { return p.requests; }), backgroundColor: COLORS.blue, borderRadius: 4 },
                        { label: 'Errors', data: providers.map(function(p) { return p.errors; }), backgroundColor: COLORS.red, borderRadius: 4 },
                    ],
                },
                options: CHART_DEFAULTS,
            });

            // Pie chart
            getOrCreateChart('chart-provider-pie', {
                type: 'pie',
                data: {
                    labels: providers.map(function(p) { return p.name; }),
                    datasets: [{
                        data: providers.map(function(p) { return p.requests; }),
                        backgroundColor: PROVIDER_COLORS.slice(0, providers.length),
                        borderWidth: 0,
                    }],
                },
                options: { responsive: true, maintainAspectRatio: false, plugins: { legend: { labels: { color: '#94a3b8' }, position: 'right' } } },
            });

            // Token distribution
            getOrCreateChart('chart-provider-tokens', {
                type: 'bar',
                data: {
                    labels: providers.map(function(p) { return p.name; }),
                    datasets: [
                        { label: 'Input', data: providers.map(function(p) { return p.input_tokens; }), backgroundColor: COLORS.cyan, borderRadius: 4 },
                        { label: 'Output', data: providers.map(function(p) { return p.output_tokens; }), backgroundColor: COLORS.purple, borderRadius: 4 },
                    ],
                },
                options: Object.assign({}, CHART_DEFAULTS, { scales: Object.assign({}, CHART_DEFAULTS.scales, { x: Object.assign({}, CHART_DEFAULTS.scales.x, { stacked: true }), y: Object.assign({}, CHART_DEFAULTS.scales.y, { stacked: true }) }) }),
            });

            // Table
            var tbody = document.querySelector('#providers-table tbody');
            tbody.innerHTML = '';
            providers.forEach(function(p) {
                var tr = document.createElement('tr');
                tr.innerHTML = '<td>' + p.name + '</td>' +
                    '<td class="number">' + formatNumber(p.requests) + '</td>' +
                    '<td class="number">' + formatNumber(p.tokens) + '</td>' +
                    '<td class="number">' + formatCost(p.cost_usd) + '</td>' +
                    '<td class="number">' + p.errors + '</td>' +
                    '<td class="number">' + formatMs(p.avg_latency_ms) + ' ms</td>' +
                    '<td class="number">' + p.active_requests + '</td>';
                tbody.appendChild(tr);
            });
        } catch (e) {
            console.error('providers failed:', e);
        }

        // Fetch health data
        try {
            var health = await fetchJSON('/api/providers/health');
            var hProviders = health.providers || [];

            // Health status cards
            var cardsDiv = document.getElementById('provider-health-cards');
            if (cardsDiv) {
                cardsDiv.innerHTML = '';
                hProviders.forEach(function(p) {
                    var statusClass = p.status === 'healthy' ? 'accent' : (p.status === 'degraded' ? '' : 'danger');
                    var card = document.createElement('div');
                    card.className = 'stat-card ' + statusClass;
                    card.innerHTML =
                        '<div class="stat-label">' + p.name + '</div>' +
                        '<div class="stat-value">' + Math.round(p.health_score) + '%</div>' +
                        '<div class="stat-sub">' + p.status + ' &middot; ' + (p.error_rate * 100).toFixed(1) + '% err</div>';
                    cardsDiv.appendChild(card);
                });
            }

            // Latency percentiles chart
            if (hProviders.length > 0) {
                getOrCreateChart('chart-provider-percentiles', {
                    type: 'bar',
                    data: {
                        labels: hProviders.map(function(p) { return p.name; }),
                        datasets: [
                            { label: 'P50', data: hProviders.map(function(p) { return p.latency_p50_ms; }), backgroundColor: COLORS.green, borderRadius: 4 },
                            { label: 'P95', data: hProviders.map(function(p) { return p.latency_p95_ms; }), backgroundColor: COLORS.yellow, borderRadius: 4 },
                            { label: 'P99', data: hProviders.map(function(p) { return p.latency_p99_ms; }), backgroundColor: COLORS.red, borderRadius: 4 },
                        ],
                    },
                    options: Object.assign({}, CHART_DEFAULTS, {
                        plugins: { legend: { labels: { color: '#94a3b8' } } },
                        scales: Object.assign({}, CHART_DEFAULTS.scales, {
                            y: Object.assign({}, CHART_DEFAULTS.scales.y, { title: { display: true, text: 'ms', color: '#64748b' } }),
                        }),
                    }),
                });

                // Cost efficiency chart
                getOrCreateChart('chart-provider-cost-efficiency', {
                    type: 'bar',
                    data: {
                        labels: hProviders.map(function(p) { return p.name; }),
                        datasets: [{
                            label: '$/1K tokens',
                            data: hProviders.map(function(p) { return p.cost_per_1k_tokens; }),
                            backgroundColor: hProviders.map(function(p, i) { return PROVIDER_COLORS[i % PROVIDER_COLORS.length]; }),
                            borderRadius: 4,
                        }],
                    },
                    options: Object.assign({}, CHART_DEFAULTS, { plugins: { legend: { display: false } } }),
                });

                // Throughput chart
                getOrCreateChart('chart-provider-throughput', {
                    type: 'bar',
                    data: {
                        labels: hProviders.map(function(p) { return p.name; }),
                        datasets: [{
                            label: 'tokens/sec',
                            data: hProviders.map(function(p) { return p.tokens_per_second; }),
                            backgroundColor: hProviders.map(function(p, i) { return PROVIDER_COLORS[(i + 2) % PROVIDER_COLORS.length]; }),
                            borderRadius: 4,
                        }],
                    },
                    options: Object.assign({}, CHART_DEFAULTS, { plugins: { legend: { display: false } } }),
                });
            }

            // Health details table
            var htbody = document.querySelector('#providers-health-table tbody');
            if (htbody) {
                htbody.innerHTML = '';
                hProviders.forEach(function(p) {
                    var statusBadge = p.status === 'healthy' ?
                        '<span class="badge badge-success">Healthy</span>' :
                        (p.status === 'degraded' ?
                            '<span class="badge badge-warn">Degraded</span>' :
                            '<span class="badge badge-error">Unhealthy</span>');
                    var tr = document.createElement('tr');
                    tr.innerHTML =
                        '<td>' + p.name + '</td>' +
                        '<td>' + statusBadge + '</td>' +
                        '<td class="number">' + Math.round(p.health_score) + '%</td>' +
                        '<td class="number">' + (p.error_rate * 100).toFixed(1) + '%</td>' +
                        '<td class="number">' + formatMs(p.latency_p50_ms) + '</td>' +
                        '<td class="number">' + formatMs(p.latency_p95_ms) + '</td>' +
                        '<td class="number">' + formatMs(p.latency_p99_ms) + '</td>' +
                        '<td class="number">' + formatCost(p.cost_per_1k_tokens) + '</td>' +
                        '<td class="number">' + formatMs(p.tokens_per_second) + '</td>';
                    htbody.appendChild(tr);
                });
            }
        } catch (e) {
            console.error('provider health failed:', e);
        }
    };

    // --- Models ---
    window.refreshModels = async function() {
        try {
            var data = await fetchJSON('/api/models');
            var models = data.models || [];

            var labels = models.map(function(m) { return m.provider + '/' + m.model; });

            getOrCreateChart('chart-model-requests', {
                type: 'bar',
                data: {
                    labels: labels,
                    datasets: [{
                        label: 'Requests',
                        data: models.map(function(m) { return m.requests; }),
                        backgroundColor: PROVIDER_COLORS.slice(0, models.length),
                        borderRadius: 4,
                    }],
                },
                options: Object.assign({}, CHART_DEFAULTS, { indexAxis: 'y', plugins: { legend: { display: false } } }),
            });

            getOrCreateChart('chart-model-cost', {
                type: 'bar',
                data: {
                    labels: labels,
                    datasets: [{
                        label: 'Cost',
                        data: models.map(function(m) { return m.cost_usd; }),
                        backgroundColor: COLORS.green,
                        borderRadius: 4,
                    }],
                },
                options: Object.assign({}, CHART_DEFAULTS, { plugins: { legend: { display: false } } }),
            });

            getOrCreateChart('chart-model-latency', {
                type: 'bar',
                data: {
                    labels: labels,
                    datasets: [{
                        label: 'Avg Latency (ms)',
                        data: models.map(function(m) { return m.avg_latency_ms; }),
                        backgroundColor: COLORS.yellow,
                        borderRadius: 4,
                    }],
                },
                options: Object.assign({}, CHART_DEFAULTS, { plugins: { legend: { display: false } } }),
            });

            var tbody = document.querySelector('#models-table tbody');
            tbody.innerHTML = '';
            models.forEach(function(m) {
                var tr = document.createElement('tr');
                tr.innerHTML = '<td>' + m.provider + '</td>' +
                    '<td>' + m.model + '</td>' +
                    '<td class="number">' + formatNumber(m.requests) + '</td>' +
                    '<td class="number">' + formatNumber(m.input_tokens) + '</td>' +
                    '<td class="number">' + formatNumber(m.output_tokens) + '</td>' +
                    '<td class="number">' + formatCost(m.cost_usd) + '</td>' +
                    '<td class="number">' + formatMs(m.avg_latency_ms) + ' ms</td>';
                tbody.appendChild(tr);
            });
        } catch (e) {
            console.error('models failed:', e);
        }
    };

    // --- Costs ---
    window.refreshCosts = async function() {
        try {
            var data = await fetchJSON('/api/costs');
            var models = data.by_model || [];

            document.getElementById('cost-total').textContent = formatCost(data.total_usd);

            getOrCreateChart('chart-cost-pie', {
                type: 'doughnut',
                data: {
                    labels: models.map(function(m) { return m.provider + '/' + m.model; }),
                    datasets: [{
                        data: models.map(function(m) { return m.cost_usd; }),
                        backgroundColor: PROVIDER_COLORS.slice(0, models.length),
                        borderWidth: 0,
                    }],
                },
                options: { responsive: true, maintainAspectRatio: false, plugins: { legend: { labels: { color: '#94a3b8' }, position: 'right' } } },
            });

            getOrCreateChart('chart-cost-avg', {
                type: 'bar',
                data: {
                    labels: models.map(function(m) { return m.provider + '/' + m.model; }),
                    datasets: [{
                        label: 'Avg Cost',
                        data: models.map(function(m) { return m.avg_cost; }),
                        backgroundColor: COLORS.green,
                        borderRadius: 4,
                    }],
                },
                options: Object.assign({}, CHART_DEFAULTS, { plugins: { legend: { display: false } } }),
            });

            var tbody = document.querySelector('#costs-table tbody');
            tbody.innerHTML = '';
            models.forEach(function(m) {
                var tr = document.createElement('tr');
                tr.innerHTML = '<td>' + m.provider + '</td>' +
                    '<td>' + m.model + '</td>' +
                    '<td class="number">' + formatCost(m.cost_usd) + '</td>' +
                    '<td class="number">' + formatNumber(m.requests) + '</td>' +
                    '<td class="number">' + formatCost(m.avg_cost) + '</td>';
                tbody.appendChild(tr);
            });
        } catch (e) {
            console.error('costs failed:', e);
        }
    };

    // --- Errors ---
    window.refreshErrors = async function() {
        try {
            var data = await fetchJSON('/api/errors');
            var byType = data.by_type || [];
            var byProv = data.by_provider || [];

            document.getElementById('errors-total').textContent = data.total_errors;

            getOrCreateChart('chart-errors-type', {
                type: 'doughnut',
                data: {
                    labels: byType.map(function(e) { return e.type; }),
                    datasets: [{
                        data: byType.map(function(e) { return e.count; }),
                        backgroundColor: [COLORS.red, COLORS.orange, COLORS.yellow, COLORS.purple, COLORS.pink, COLORS.cyan],
                        borderWidth: 0,
                    }],
                },
                options: { responsive: true, maintainAspectRatio: false, plugins: { legend: { labels: { color: '#94a3b8' }, position: 'right' } } },
            });

            getOrCreateChart('chart-errors-provider', {
                type: 'bar',
                data: {
                    labels: byProv.map(function(e) { return e.provider; }),
                    datasets: [{
                        label: 'Errors',
                        data: byProv.map(function(e) { return e.count; }),
                        backgroundColor: COLORS.red,
                        borderRadius: 4,
                    }],
                },
                options: Object.assign({}, CHART_DEFAULTS, { plugins: { legend: { display: false } } }),
            });

            var tbody = document.querySelector('#errors-table tbody');
            tbody.innerHTML = '';
            byType.forEach(function(e) {
                var tr = document.createElement('tr');
                tr.innerHTML = '<td>' + e.type + '</td><td class="number">' + e.count + '</td>';
                tbody.appendChild(tr);
            });
        } catch (e) {
            console.error('errors failed:', e);
        }
    };

    // --- Traces ---
    window.refreshTraces = async function() {
        try {
            var provider = document.getElementById('trace-filter-provider').value;
            var status = document.getElementById('trace-filter-status').value;
            var limit = document.getElementById('trace-filter-limit').value;

            var url = '/api/traces?limit=' + limit;
            if (provider) url += '&provider=' + encodeURIComponent(provider);
            if (status) url += '&status=' + encodeURIComponent(status);

            var data = await fetchJSON(url);
            var traces = data.traces || [];

            // Populate provider filter if empty
            var sel = document.getElementById('trace-filter-provider');
            if (sel.options.length <= 1 && traces.length > 0) {
                var provs = {};
                traces.forEach(function(t) { provs[t.provider] = true; });
                Object.keys(provs).sort().forEach(function(p) {
                    var opt = document.createElement('option');
                    opt.value = p;
                    opt.textContent = p;
                    sel.appendChild(opt);
                });
            }

            var tbody = document.querySelector('#traces-table tbody');
            tbody.innerHTML = '';
            traces.forEach(function(t) {
                var tr = document.createElement('tr');
                var statusBadge = t.status === 'success' ?
                    '<span class="badge badge-success">OK</span>' :
                    '<span class="badge badge-error">ERR</span>';
                var time = t.timestamp ? new Date(t.timestamp).toLocaleTimeString() : '-';
                tr.innerHTML =
                    '<td title="' + t.id + '">' + t.id.substring(0, 8) + '</td>' +
                    '<td>' + time + '</td>' +
                    '<td>' + t.provider + '</td>' +
                    '<td>' + t.model + '</td>' +
                    '<td>' + statusBadge + '</td>' +
                    '<td class="number">' + (t.total_tokens || 0) + '</td>' +
                    '<td class="number">' + formatCost(t.cost_usd || 0) + '</td>' +
                    '<td class="number">' + (t.latency_ms ? formatMs(t.latency_ms) + ' ms' : '-') + '</td>';
                tbody.appendChild(tr);
            });
        } catch (e) {
            console.error('traces failed:', e);
        }
    };

    // --- SSE (real-time updates) ---
    function setupSSE() {
        var statusEl = document.getElementById('sse-status');
        var evtSource = new EventSource('/api/events');

        evtSource.addEventListener('overview', function(e) {
            statusEl.className = 'sse-status connected';
            statusEl.querySelector('.status-text').textContent = 'Live';

            try {
                var data = JSON.parse(e.data);
                // Update overview cards if on overview page
                if (document.getElementById('page-overview').classList.contains('active')) {
                    document.getElementById('stat-requests').textContent = formatNumber(data.total_requests);
                    document.getElementById('stat-tokens').textContent = formatNumber(data.total_tokens);
                    document.getElementById('stat-input-tokens').textContent = formatNumber(data.input_tokens);
                    document.getElementById('stat-output-tokens').textContent = formatNumber(data.output_tokens);
                    document.getElementById('stat-cost').textContent = formatCost(data.total_cost_usd);
                    document.getElementById('stat-active').textContent = data.active_requests;
                    document.getElementById('stat-errors').textContent = data.total_errors;
                    document.getElementById('stat-latency').textContent = formatMs(data.avg_latency_ms);
                    document.getElementById('stat-providers').textContent = data.provider_count;
                    document.getElementById('stat-models').textContent = data.model_count;
                    if (data.timestamp) {
                        document.getElementById('overview-timestamp').textContent = 'Live: ' + new Date(data.timestamp).toLocaleTimeString();
                    }
                }
            } catch (err) {
                console.error('SSE parse error:', err);
            }
        });

        evtSource.onerror = function() {
            statusEl.className = 'sse-status error';
            statusEl.querySelector('.status-text').textContent = 'Disconnected';
        };
    }

    // --- Init ---
    refreshOverview();
    setupSSE();
})();