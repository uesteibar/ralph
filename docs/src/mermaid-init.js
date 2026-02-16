// Load Mermaid and render diagrams in ```mermaid code blocks.
(function () {
  var script = document.createElement("script");
  script.src =
    "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js";
  script.onload = function () {
    mermaid.initialize({ startOnLoad: false });
    var blocks = document.querySelectorAll("code.language-mermaid");
    blocks.forEach(function (block) {
      var pre = block.parentElement;
      var container = document.createElement("div");
      container.className = "mermaid";
      container.textContent = block.textContent;
      pre.parentElement.replaceChild(container, pre);
    });
    mermaid.run();
  };
  document.head.appendChild(script);
})();
