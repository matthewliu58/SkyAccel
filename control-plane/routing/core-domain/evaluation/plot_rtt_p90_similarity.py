import re
import matplotlib.pyplot as plt

def parse_results(filepath):
    """Parse results from a test results file."""
    data = {
        'p90_rtt': [],
        'prefix3_sim': []
    }
    
    with open(filepath, 'r') as f:
        content = f.read()
    
    pattern = r'--- Run (\d+) ---\s*(.*?)(?=\n--- Run \d+ ---|\Z)'
    matches = re.findall(pattern, content, re.DOTALL)
    
    for run_num, block in matches:
        match = re.search(r'P90 RTT\s*:\s*([\d.]+)\s*ms', block)
        if match:
            data['p90_rtt'].append(float(match.group(1)))
        
        match = re.search(r'Prefix-3 similarity \(backbone sharing\):\s*([\d.]+)', block)
        if match:
            data['prefix3_sim'].append(float(match.group(1)))
    
    return data

# Parse all three result files
lifestyle_data = parse_results('livenet_test_results.txt')
onewan_data = parse_results('onewan_multi_results.txt')
carousel_data = parse_results('carousel_greed_results.txt')

# Create figure with 2 subplots (RTT P90 and Prefix-3 similarity)
fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(14, 6))
plt.rcParams.update({'font.size': 12})

# Colors for each algorithm
box_colors = ['#a8d8ea', '#ffb3b3', '#98fb98']
scatter_colors = ['#1f77b4', '#ff7f0e', '#2ca02c']
labels = ['LiveStyle', 'ONEWAN', 'Carousel Greed']

# === RTT P90 Comparison Plot ===
ax1.set_title('RTT P90 Distribution', fontsize=14, fontweight='bold')
ax1.set_ylabel('RTT P90 (ms)', fontsize=12)

rtt_data = [
    lifestyle_data['p90_rtt'],
    onewan_data['p90_rtt'],
    carousel_data['p90_rtt']
]

bp1 = ax1.boxplot(rtt_data, tick_labels=labels,
                  patch_artist=True, showmeans=True, widths=0.6)

for patch, color in zip(bp1['boxes'], box_colors):
    patch.set_facecolor(color)
    patch.set_alpha(0.7)

for i, values in enumerate(rtt_data):
    x = [i+1] * len(values)
    ax1.scatter(x, values, alpha=0.6, s=40, color=scatter_colors[i], zorder=3)

ax1.grid(True, linestyle='--', alpha=0.7)

# === Prefix-3 Similarity Comparison Plot ===
ax2.set_title('Prefix-3 Similarity (Backbone Sharing)', fontsize=14, fontweight='bold')
ax2.set_ylabel('Similarity Score', fontsize=12)

prefix_data = [
    lifestyle_data['prefix3_sim'],
    onewan_data['prefix3_sim'],
    carousel_data['prefix3_sim']
]

bp2 = ax2.boxplot(prefix_data, tick_labels=labels,
                  patch_artist=True, showmeans=True, widths=0.6)

for patch, color in zip(bp2['boxes'], box_colors):
    patch.set_facecolor(color)
    patch.set_alpha(0.7)

for i, values in enumerate(prefix_data):
    x = [i+1] * len(values)
    ax2.scatter(x, values, alpha=0.6, s=40, color=scatter_colors[i], zorder=3)

ax2.grid(True, linestyle='--', alpha=0.7)

# Add legend
from matplotlib.patches import Patch
legend_elements = [
    Patch(facecolor=box_colors[0], edgecolor=scatter_colors[0], label='LiveStyle'),
    Patch(facecolor=box_colors[1], edgecolor=scatter_colors[1], label='ONEWAN'),
    Patch(facecolor=box_colors[2], edgecolor=scatter_colors[2], label='Carousel Greed')
]
ax1.legend(handles=legend_elements, loc='upper left', fontsize=10)

plt.tight_layout(pad=3.0)
plt.savefig('rtt_p90_similarity_comparison.png', dpi=300, bbox_inches='tight')
print("RTT P90 和 Prefix-3 Similarity 对比图已保存为 rtt_p90_similarity_comparison.png")

# Print summary statistics
print("\n" + "="*70)
print("三算法对比汇总")
print("="*70)

print("\n--- RTT P90 (ms) ---")
print(f"LiveStyle:    {sum(lifestyle_data['p90_rtt'])/len(lifestyle_data['p90_rtt']):.2f}")
print(f"ONEWAN:       {sum(onewan_data['p90_rtt'])/len(onewan_data['p90_rtt']):.2f}")
print(f"Carousel:     {sum(carousel_data['p90_rtt'])/len(carousel_data['p90_rtt']):.2f}")

print("\n--- Prefix-3 Similarity (Backbone Sharing) ---")
print(f"LiveStyle:    {sum(lifestyle_data['prefix3_sim'])/len(lifestyle_data['prefix3_sim']):.3f}")
print(f"ONEWAN:       {sum(onewan_data['prefix3_sim'])/len(onewan_data['prefix3_sim']):.3f}")
print(f"Carousel:     {sum(carousel_data['prefix3_sim'])/len(carousel_data['prefix3_sim']):.3f}")

print("\n--- 分析结论 ---")
print("• RTT P90 越低，表示延迟稳定性越好")
print("• Prefix-3 similarity 越低，表示路径分散性越好")