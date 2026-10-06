#if targetEnvironment(macCatalyst)
import UIKit
import ImageIO

/// The console's lightbox (threads.js openImage / app.css .lightbox) for the
/// desktop app: a click on a picture in a chat covers the whole window with
/// a dark ground and the file as big as the window allows, a caption with
/// the file name and an "open the file" link under it; click anywhere (or
/// Esc) closes. One at a time. A window-wide overlay, not a sheet, so it
/// sits over the top bar and the composer the way the web one does (the owner
/// 2026-09-30: "I used to be able to click into an image to kind of enlarge
/// it so I can see what's going on").
///
/// The picture is decoded at the file's full size here, cached apart from
/// the cell thumbnails (`BlobImage.cache`) — the cell needs a few hundred
/// pixels, this needs them all, and neither should evict the other.
@MainActor
enum MacLightbox {
    static let full = NSCache<NSString, UIImage>()
    private static weak var current: LightboxView?

    static func open(ref: String, hub: HubClient) {
        close()
        guard let window = UIApplication.shared.connectedScenes
            .compactMap({ ($0 as? UIWindowScene)?.keyWindow ?? ($0 as? UIWindowScene)?.windows.first })
            .first else { return }
        let name = String(ref.split(separator: "/").last ?? "image")
        let box = LightboxView(name: name)
        box.frame = window.bounds
        box.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        window.addSubview(box)
        box.becomeFirstResponder()
        current = box
        if let img = full.object(forKey: ref as NSString) { box.show(img) }
        Task {
            guard let data = try? await hub.blob(ref) else { box.fail(); return }
            box.data = data
            if box.image == nil {
                guard let img = await decodeFull(data) else { box.fail(); return }
                full.setObject(img, forKey: ref as NSString)
                box.show(img)
            }
        }
    }

    /// A file on this Mac: the same box,
    /// no hub between.
    static func open(file url: URL) {
        close()
        guard let window = UIApplication.shared.connectedScenes
            .compactMap({ ($0 as? UIWindowScene)?.keyWindow ?? ($0 as? UIWindowScene)?.windows.first })
            .first else { return }
        let box = LightboxView(name: url.lastPathComponent)
        box.frame = window.bounds
        box.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        window.addSubview(box)
        box.becomeFirstResponder()
        current = box
        let key = url.path as NSString
        if let img = full.object(forKey: key) { box.show(img) }
        Task {
            guard let data = try? Data(contentsOf: url) else { box.fail(); return }
            box.data = data
            if box.image == nil {
                guard let img = await decodeFull(data) else { box.fail(); return }
                full.setObject(img, forKey: key)
                box.show(img)
            }
        }
    }

    /// A picture not sent yet (the chip in a composer's strip, AttachBar):
    /// the same box, straight from memory. "open the file" writes it out as
    /// a JPEG first.
    static func open(image: UIImage, name: String) {
        close()
        guard let window = UIApplication.shared.connectedScenes
            .compactMap({ ($0 as? UIWindowScene)?.keyWindow ?? ($0 as? UIWindowScene)?.windows.first })
            .first else { return }
        let box = LightboxView(name: name)
        box.frame = window.bounds
        box.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        window.addSubview(box)
        box.becomeFirstResponder()
        current = box
        box.show(image)
        box.data = image.jpegData(compressionQuality: 0.9)
    }

    static func close() {
        current?.resignFirstResponder()
        current?.removeFromSuperview()
        current = nil
    }

    /// A screenshot run (ops/mac-screens.sh, LIFE_SHOT) can ask for the
    /// lightbox open on a picture: LIFE_SHOT_IMAGE=1 takes the first picture
    /// that loads, LIFE_SHOT_IMAGE=<suffix of a ref> that one.
    static func openIfShotAsks(ref: String, hub: HubClient) {
        guard current == nil,
              let want = ProcessInfo.processInfo.environment["LIFE_SHOT_IMAGE"], !want.isEmpty,
              want == "1" || ref.hasSuffix(want) else { return }
        open(ref: ref, hub: hub)
    }

    /// Every pixel of the file, EXIF-rotated, decoded off the main actor.
    /// Uploads from the app are at most 1600 px a side (AttachmentDraft.jpeg),
    /// a Mac screenshot 2880×1800: tens of megabytes at worst, fine here.
    nonisolated static func decodeFull(_ data: Data) async -> UIImage? {
        await Task.detached(priority: .userInitiated) {
            guard let src = CGImageSourceCreateWithData(data as CFData, nil) else { return nil }
            let props = CGImageSourceCopyPropertiesAtIndex(src, 0, nil) as? [CFString: Any]
            let w = props?[kCGImagePropertyPixelWidth] as? Int ?? 0
            let h = props?[kCGImagePropertyPixelHeight] as? Int ?? 0
            let opts: [CFString: Any] = [
                kCGImageSourceCreateThumbnailFromImageAlways: true,
                kCGImageSourceCreateThumbnailWithTransform: true,
                kCGImageSourceShouldCacheImmediately: true,
                kCGImageSourceThumbnailMaxPixelSize: max(w, h, 1),
            ]
            guard let cg = CGImageSourceCreateThumbnailAtIndex(src, 0, opts as CFDictionary) else { return nil }
            return UIImage(cgImage: cg)
        }.value
    }
}

/// app.css .lightbox, in UIKit: rgba(0,0,0,.86) ground, the picture inside
/// 24 pt of padding and above a 90 pt band for the caption, white behind it
/// (a transparent PNG reads), 8 pt corners, a soft shadow; caption #e8eaee
/// 12.5 pt with the link in #9ec1ff. The image view is sized by hand to the
/// fitted picture so the white and the corners hug the picture, not the
/// letterbox an aspect-fit view would leave.
final class LightboxView: UIView, UIGestureRecognizerDelegate {
    private let imageView = UIImageView()
    private let spinner = UIActivityIndicatorView(style: .large)
    private let caption = UILabel()
    private let openLink = UIButton(type: .custom)
    private lazy var cap = UIStackView(arrangedSubviews: [caption, openLink])
    private let name: String
    var data: Data?
    var image: UIImage? { imageView.image }

    init(name: String) {
        self.name = name
        super.init(frame: .zero)
        backgroundColor = UIColor(white: 0, alpha: 0.86)

        imageView.contentMode = .scaleAspectFit
        imageView.backgroundColor = .white
        imageView.layer.cornerRadius = 8
        imageView.layer.masksToBounds = true
        imageView.isHidden = true
        addSubview(imageView)
        // The shadow needs a layer that does not clip: a sibling under the picture.
        shadow.backgroundColor = .clear
        shadow.layer.shadowColor = UIColor.black.cgColor
        shadow.layer.shadowOpacity = 0.5
        shadow.layer.shadowRadius = 20
        shadow.layer.shadowOffset = CGSize(width: 0, height: 10)
        insertSubview(shadow, belowSubview: imageView)

        spinner.color = .white
        spinner.startAnimating()
        addSubview(spinner)

        caption.text = name
        caption.font = .systemFont(ofSize: 12.5)
        caption.textColor = UIColor(red: 0xe8 / 255, green: 0xea / 255, blue: 0xee / 255, alpha: 1)
        // A bare link, not a Mac push button (`.system` draws a bordered one
        // on Catalyst).
        openLink.setTitle("open the file", for: .normal)
        openLink.titleLabel?.font = .systemFont(ofSize: 12.5)
        openLink.setTitleColor(UIColor(red: 0x9e / 255, green: 0xc1 / 255, blue: 0xff / 255, alpha: 1), for: .normal)
        openLink.addTarget(self, action: #selector(openFile), for: .touchUpInside)
        cap.axis = .horizontal
        cap.spacing = 14
        cap.alignment = .center
        addSubview(cap)

        let tap = UITapGestureRecognizer(target: self, action: #selector(closeTapped))
        tap.delegate = self
        addGestureRecognizer(tap)
    }
    private let shadow = UIView()

    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func show(_ img: UIImage) {
        imageView.image = img
        imageView.isHidden = false
        spinner.stopAnimating()
        spinner.isHidden = true
        setNeedsLayout()
    }

    func fail() {
        spinner.stopAnimating()
        spinner.isHidden = true
        caption.text = "\(name) — photo unavailable"
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        spinner.center = CGPoint(x: bounds.midX, y: bounds.midY)
        let capSize = cap.systemLayoutSizeFitting(UIView.layoutFittingCompressedSize)
        guard let img = imageView.image, img.size.width > 0, img.size.height > 0 else {
            cap.frame = CGRect(x: ((bounds.width - capSize.width) / 2).rounded(), y: bounds.midY + 40,
                               width: capSize.width, height: capSize.height)
            return
        }
        // max-width: 96vw; max-height: calc(100vh - 90px). Like the browser,
        // one point per pixel of the file and no larger — a 1600 px upload
        // fills a 1280 pt window, a 600 px one sits at 600 pt.
        let natural = CGSize(width: img.size.width * img.scale, height: img.size.height * img.scale)
        let room = CGSize(width: bounds.width * 0.96, height: bounds.height - 90)
        let k = min(1, room.width / natural.width, room.height / natural.height)
        let size = CGSize(width: (natural.width * k).rounded(), height: (natural.height * k).rounded())
        // The picture and its caption as one centred column with a 10 pt gap.
        let block = size.height + 10 + capSize.height
        let y = ((bounds.height - block) / 2).rounded()
        imageView.frame = CGRect(x: ((bounds.width - size.width) / 2).rounded(), y: y, width: size.width, height: size.height)
        shadow.frame = imageView.frame
        shadow.layer.shadowPath = UIBezierPath(roundedRect: shadow.bounds, cornerRadius: 8).cgPath
        cap.frame = CGRect(x: ((bounds.width - capSize.width) / 2).rounded(), y: imageView.frame.maxY + 10,
                           width: capSize.width, height: capSize.height)
    }

    // A click anywhere but the link closes (the web's `e.target.tagName !== 'A'`).
    func gestureRecognizer(_ g: UIGestureRecognizer, shouldReceive touch: UITouch) -> Bool {
        !(touch.view is UIControl)
    }
    @objc private func closeTapped() { MacLightbox.close() }

    // Esc, while the overlay is up.
    override var canBecomeFirstResponder: Bool { true }
    override var keyCommands: [UIKeyCommand]? {
        [UIKeyCommand(input: UIKeyCommand.inputEscape, modifierFlags: [], action: #selector(escape))]
    }
    @objc private func escape() { MacLightbox.close() }

    /// "open the file": the blob under its own name in the temp dir, handed
    /// to the Mac (Preview, or whatever owns the extension) — the same road
    /// as a document chip's Quick Look copy (BlobDocument.fetch).
    @objc private func openFile() {
        guard let data else { return }
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("blobs", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let url = dir.appendingPathComponent(name)
        guard (try? data.write(to: url, options: .atomic)) != nil else { return }
        UIApplication.shared.open(url)
    }
}
#endif
