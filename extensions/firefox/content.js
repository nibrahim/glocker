// Page content analysis
const contentKeywords = ['trigger1', 'trigger2'];

function analyzeContent() {
  const text = document.body ? document.body.textContent.toLowerCase() : '';
  
  for (let keyword of contentKeywords) {
    if (text.includes(keyword)) {
      fetch('http://127.0.0.1/report', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({
          url: window.location.href,
          domain: window.location.hostname,
          trigger: `content-keyword:${keyword}`,
          timestamp: Date.now()
        })
      }).catch(() => {});
      
      break; // Only report once per page
    }
  }
}

// Run on page load
if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', analyzeContent);
} else {
  analyzeContent();
}
