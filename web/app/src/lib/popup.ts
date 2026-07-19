export function popupContent(
	title: string,
	lines: string[],
	doc: Document = document,
): HTMLElement {
	const popup = doc.createElement("div");
	const heading = doc.createElement("strong");
	heading.textContent = title;
	popup.append(heading);
	for (const line of lines)
		popup.append(doc.createElement("br"), doc.createTextNode(line));
	return popup;
}
