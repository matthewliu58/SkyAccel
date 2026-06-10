import matplotlib
matplotlib.use('Agg')  # Use non-interactive backend for headless environments

import numpy as np
import matplotlib.pyplot as plt

# Set font for English labels
plt.rcParams['font.sans-serif'] = ['DejaVu Sans', 'Arial']
plt.rcParams['axes.unicode_minus'] = False

# -----------------------------------------------------------------------------
# CPU Risk Curve
# -----------------------------------------------------------------------------
# CPU thresholds
CPUMid = 60.0
CPUHigh = 80.0
cpuPower = 2.0

# x-axis: real CPU values [0, 100]
cpu_x = np.linspace(0, 100, 200)

# CPU risk calculation
cpu_risk = np.zeros_like(cpu_x)
for i, cpu in enumerate(cpu_x):
    if cpu < CPUMid:
        cpu_risk[i] = 0.0
    else:
        cpu_ratio = (cpu - CPUMid) / (CPUHigh - CPUMid)
        cpu_risk[i] = np.power(cpu_ratio, cpuPower)

# -----------------------------------------------------------------------------
# Latency Risk Curve
# -----------------------------------------------------------------------------
# Latency parameters
latencyMax = 20.0
latPower = 1.5

# x-axis: real latency values [0, 50] ms
lat_x = np.linspace(0, 50, 200)

# Latency risk calculation
lat_ratio = lat_x / latencyMax
lat_risk = np.power(lat_ratio, latPower)

# -----------------------------------------------------------------------------
# Plot CPU Risk
# -----------------------------------------------------------------------------
fig1, ax1 = plt.subplots(figsize=(10, 6))
ax1.plot(cpu_x, cpu_risk, 'b-', linewidth=2)

# Add reference lines
ax1.axvline(x=CPUMid, color='green', linestyle='--', alpha=0.7, label=f'CPUMid = {CPUMid}%')
ax1.axvline(x=CPUHigh, color='orange', linestyle='--', alpha=0.7, label=f'CPUHigh = {CPUHigh}%')
ax1.axhline(y=1.0, color='red', linestyle=':', alpha=0.7, label='Risk = 1.0')

# Mark key points
ax1.scatter([CPUMid], [0.0], color='green', s=100, zorder=5)
ax1.scatter([CPUHigh], [1.0], color='orange', s=100, zorder=5)
ax1.scatter([100.0], [((100-CPUMid)/(CPUHigh-CPUMid))**cpuPower], color='red', s=100, zorder=5)

ax1.annotate(f'({CPUMid}%, 0)', xy=(CPUMid, 0), xytext=(CPUMid+2, 0.2), fontsize=10, color='green')
ax1.annotate(f'({CPUHigh}%, 1.0)', xy=(CPUHigh, 1.0), xytext=(CPUHigh+2, 1.1), fontsize=10, color='orange')
ax1.annotate(f'(100%, {((100-CPUMid)/(CPUHigh-CPUMid))**cpuPower:.1f})', 
             xy=(100, ((100-CPUMid)/(CPUHigh-CPUMid))**cpuPower), 
             xytext=(85, 3.5), fontsize=10, color='red')

# Figure decoration
ax1.set_xlabel('CPU Pressure (%)', fontsize=12)
ax1.set_ylabel('CPU Risk', fontsize=12)
ax1.set_title('CPU Risk Curve (power=2.0)', fontsize=14, fontweight='bold')
ax1.legend(fontsize=11)
ax1.grid(True, alpha=0.3)
ax1.set_xlim(0, 100)
ax1.set_ylim(0, 4.5)

fig1.tight_layout()
fig1.savefig('cpu_risk_curve.png', dpi=300, bbox_inches='tight')

# -----------------------------------------------------------------------------
# Plot Latency Risk
# -----------------------------------------------------------------------------
fig2, ax2 = plt.subplots(figsize=(10, 6))
ax2.plot(lat_x, lat_risk, 'r-', linewidth=2)

# Add reference lines
ax2.axvline(x=latencyMax, color='green', linestyle='--', alpha=0.7, label=f'latencyMax = {latencyMax}ms')
ax2.axhline(y=1.0, color='orange', linestyle=':', alpha=0.7, label='Risk = 1.0')

# Mark key points
ax2.scatter([latencyMax], [1.0], color='green', s=100, zorder=5)
ax2.scatter([2*latencyMax], [np.power(2, latPower)], color='red', s=100, zorder=5)

ax2.annotate(f'({latencyMax}ms, 1.0)', xy=(latencyMax, 1.0), xytext=(latencyMax+2, 1.1), fontsize=10, color='green')
ax2.annotate(f'({2*latencyMax}ms, {np.power(2, latPower):.2f})', 
             xy=(2*latencyMax, np.power(2, latPower)), 
             xytext=(2*latencyMax+2, np.power(2, latPower)+0.2), fontsize=10, color='red')

# Figure decoration
ax2.set_xlabel('Latency (ms)', fontsize=12)
ax2.set_ylabel('Latency Risk', fontsize=12)
ax2.set_title('Latency Risk Curve (power=1.5)', fontsize=14, fontweight='bold')
ax2.legend(fontsize=11)
ax2.grid(True, alpha=0.3)
ax2.set_xlim(0, 50)
ax2.set_ylim(0, 3.5)

fig2.tight_layout()
fig2.savefig('latency_risk_curve.png', dpi=300, bbox_inches='tight')

print("CPU risk curve saved as cpu_risk_curve.png")
print("Latency risk curve saved as latency_risk_curve.png")
