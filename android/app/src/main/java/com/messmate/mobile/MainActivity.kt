package com.messmate.mobile

import android.Manifest
import android.annotation.SuppressLint
import android.app.DownloadManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.graphics.Color
import android.content.res.ColorStateList
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.animation.ObjectAnimator
import android.animation.ValueAnimator
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.TextView
import android.provider.Settings
import android.webkit.PermissionRequest
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.JavascriptInterface
import android.webkit.WebViewClient
import android.webkit.WebSettings
import android.widget.Toast
import android.widget.FrameLayout
import android.widget.ProgressBar
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

class MainActivity : AppCompatActivity() {
    private lateinit var webView: WebView
    private lateinit var splashOverlay: FrameLayout
    private var pendingWebPermission: PermissionRequest? = null
    private var checkingUpdate = false

    private val cameraPermissionLauncher =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
            val request = pendingWebPermission
            pendingWebPermission = null
            if (granted && request != null && isTrustedOrigin(request.origin)) {
                request.grant(arrayOf(PermissionRequest.RESOURCE_VIDEO_CAPTURE))
            } else request?.deny()
        }

    private val updateReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            if (intent.action != DownloadManager.ACTION_DOWNLOAD_COMPLETE) return
            val id = intent.getLongExtra(DownloadManager.EXTRA_DOWNLOAD_ID, -1L)
            val prefs = getSharedPreferences(UPDATE_PREFS, MODE_PRIVATE)
            if (id < 0 || id != prefs.getLong(PENDING_DOWNLOAD_ID, -2L)) return
            val manager = getSystemService(DOWNLOAD_SERVICE) as DownloadManager
            manager.query(DownloadManager.Query().setFilterById(id)).use { cursor ->
                if (!cursor.moveToFirst()) return
                if (cursor.getInt(cursor.getColumnIndexOrThrow(DownloadManager.COLUMN_STATUS)) != DownloadManager.STATUS_SUCCESSFUL) {
                    prefs.edit().remove(PENDING_DOWNLOAD_ID).apply()
                    Toast.makeText(this@MainActivity, "MessMate update download failed. Please try again.", Toast.LENGTH_LONG).show()
                    return
                }
            }
            installDownloadedUpdate(id)
        }
    }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.statusBarColor = android.graphics.Color.rgb(5, 5, 5)
        window.navigationBarColor = android.graphics.Color.rgb(5, 5, 5)
        ContextCompat.registerReceiver(this, updateReceiver, IntentFilter(DownloadManager.ACTION_DOWNLOAD_COMPLETE), ContextCompat.RECEIVER_NOT_EXPORTED)

        webView = WebView(this)
        webView.settings.javaScriptEnabled = true
        webView.settings.domStorageEnabled = true
        webView.settings.cacheMode = WebSettings.LOAD_NO_CACHE
        webView.settings.mediaPlaybackRequiresUserGesture = true
        webView.settings.allowFileAccess = false
        webView.settings.allowContentAccess = false
        webView.addJavascriptInterface(object {
            @JavascriptInterface
            fun dialPhone(number: String) {
                runOnUiThread {
                    val cleaned = number.filter { it.isDigit() || it == '+' || it == '*' || it == '#' }
                    if (cleaned.isBlank() || cleaned.none { it.isDigit() }) {
                        Toast.makeText(this@MainActivity, "Invalid phone number.", Toast.LENGTH_SHORT).show()
                        return@runOnUiThread
                    }
                    try {
                        startActivity(Intent(Intent.ACTION_DIAL, Uri.parse("tel:" + Uri.encode(cleaned))))
                    } catch (_: Exception) {
                        Toast.makeText(this@MainActivity, "No phone dialer is available.", Toast.LENGTH_LONG).show()
                    }
                }
            }
        }, "MessMateNative")
        webView.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                val uri = request.url
                if (uri.scheme == "tel") {
                    try { startActivity(Intent(Intent.ACTION_DIAL, uri)) }
                    catch (_: Exception) { Toast.makeText(this@MainActivity, "No phone dialer is available.", Toast.LENGTH_LONG).show() }
                    return true
                }
                return !isTrustedUrl(uri)
            }

            override fun onPageFinished(view: WebView, url: String) {
                super.onPageFinished(view, url)
                if (isTrustedOrigin(Uri.parse(url)) && ::splashOverlay.isInitialized && splashOverlay.visibility == View.VISIBLE) {
                    splashOverlay.animate().alpha(0f).setDuration(180).withEndAction {
                        splashOverlay.visibility = View.GONE
                    }.start()
                }
            }
        }
        webView.webChromeClient = object : WebChromeClient() {
            override fun onPermissionRequest(request: PermissionRequest) {
                runOnUiThread {
                    if (!isTrustedOrigin(request.origin) || request.resources.none { it == PermissionRequest.RESOURCE_VIDEO_CAPTURE }) {
                        request.deny()
                        return@runOnUiThread
                    }
                    if (ContextCompat.checkSelfPermission(this@MainActivity, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) {
                        request.grant(arrayOf(PermissionRequest.RESOURCE_VIDEO_CAPTURE))
                    } else {
                        pendingWebPermission = request
                        cameraPermissionLauncher.launch(Manifest.permission.CAMERA)
                    }
                }
            }
        }
        val root = FrameLayout(this)
        root.addView(webView, FrameLayout.LayoutParams(
            FrameLayout.LayoutParams.MATCH_PARENT,
            FrameLayout.LayoutParams.MATCH_PARENT
        ))

        splashOverlay = FrameLayout(this).apply {
            // Keep the entire screen neon green while the WebView is loading.
            setBackgroundColor(Color.rgb(198, 255, 0))

            val splashContent = LinearLayout(this@MainActivity).apply {
                orientation = LinearLayout.VERTICAL
                gravity = Gravity.CENTER
                setPadding(dp(24), dp(24), dp(24), dp(24))
            }

            splashContent.addView(ImageView(this@MainActivity).apply {
                setImageResource(R.drawable.messmate_icon)
                scaleType = ImageView.ScaleType.FIT_CENTER
                contentDescription = "MessMate"
            }, LinearLayout.LayoutParams(dp(132), dp(132)))

            val dots = LinearLayout(this@MainActivity).apply {
                orientation = LinearLayout.HORIZONTAL
                gravity = Gravity.CENTER
            }
            repeat(3) { index ->
                val dot = TextView(this@MainActivity).apply {
                    text = "•"
                    textSize = 32f
                    setTextColor(Color.BLACK)
                    gravity = Gravity.CENTER
                    includeFontPadding = false
                }
                val dotParams = LinearLayout.LayoutParams(dp(22), dp(30)).apply {
                    leftMargin = dp(2)
                    rightMargin = dp(2)
                    topMargin = dp(16)
                }
                dots.addView(dot, dotParams)
                ObjectAnimator.ofFloat(dot, View.TRANSLATION_Y, 0f, -dp(8).toFloat(), 0f).apply {
                    duration = 620L
                    startDelay = index * 150L
                    repeatCount = ValueAnimator.INFINITE
                    repeatMode = ValueAnimator.RESTART
                    start()
                }
            }
            splashContent.addView(dots)
            addView(splashContent, FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.WRAP_CONTENT,
                FrameLayout.LayoutParams.WRAP_CONTENT,
                Gravity.CENTER
            ))
        }
        root.addView(splashOverlay, FrameLayout.LayoutParams(
            FrameLayout.LayoutParams.MATCH_PARENT,
            FrameLayout.LayoutParams.MATCH_PARENT
        ))
        setContentView(root)
        if (savedInstanceState == null) webView.loadUrl(APP_URL) else webView.restoreState(savedInstanceState)
        checkForUpdate()
    }

    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()

    private fun checkForUpdate() {
        if (checkingUpdate) return
        checkingUpdate = true
        Thread {
            var connection: HttpURLConnection? = null
            try {
                connection = (URL(LATEST_RELEASE_API).openConnection() as HttpURLConnection).apply {
                    connectTimeout = 7000
                    readTimeout = 7000
                    setRequestProperty("Accept", "application/vnd.github+json")
                    setRequestProperty("User-Agent", "MessMate-Android")
                }
                if (connection.responseCode != HttpURLConnection.HTTP_OK) return@Thread
                val releases = org.json.JSONArray(connection.inputStream.bufferedReader().use { it.readText() })
                var latest = ""
                var apkUrl = ""
                for (releaseIndex in 0 until releases.length()) {
                    val release = releases.getJSONObject(releaseIndex)
                    if (release.optBoolean("draft") || release.optBoolean("prerelease")) continue
                    val version = release.optString("tag_name").removePrefix("android-v").removePrefix("v")
                    if (compareVersions(version, BuildConfig.VERSION_NAME) <= 0) continue
                    val assets = release.optJSONArray("assets") ?: continue
                    for (assetIndex in 0 until assets.length()) {
                        val asset = assets.getJSONObject(assetIndex)
                        if (asset.optString("name") == APK_ASSET_NAME) {
                            val candidate = asset.optString("browser_download_url")
                            if (candidate.startsWith("https://github.com/")) {
                                latest = version
                                apkUrl = candidate
                            }
                            break
                        }
                    }
                    if (apkUrl.isNotEmpty()) break
                }
                if (apkUrl.isEmpty()) return@Thread
                runOnUiThread { showUpdateDialog(latest, apkUrl) }
            } catch (_: Exception) {
                // Keep the app usable when the update service is unavailable.
            } finally {
                connection?.disconnect()
                runOnUiThread { checkingUpdate = false }
            }
        }.start()
    }

    private fun compareVersions(a: String, b: String): Int {
        val left = a.split(".").map { it.toIntOrNull() ?: 0 }
        val right = b.split(".").map { it.toIntOrNull() ?: 0 }
        for (i in 0 until maxOf(left.size, right.size)) {
            val difference = left.getOrElse(i) { 0 }.compareTo(right.getOrElse(i) { 0 })
            if (difference != 0) return difference
        }
        return 0
    }

    private fun showUpdateDialog(version: String, apkUrl: String) {
        if (isFinishing) return
        AlertDialog.Builder(this)
            .setTitle("MessMate update available")
            .setMessage("Version $version is ready. Download the update now?")
            .setNegativeButton("Later", null)
            .setPositiveButton("Update") { _, _ -> downloadUpdate(version, apkUrl) }
            .show()
    }

    private fun downloadUpdate(version: String, apkUrl: String) {
        try {
            val request = DownloadManager.Request(Uri.parse(apkUrl))
                .setTitle("MessMate $version update")
                .setDescription("Downloading the latest MessMate app")
                .setMimeType("application/vnd.android.package-archive")
                .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
                .setAllowedOverMetered(true)
                .setAllowedOverRoaming(false)
            val id = (getSystemService(DOWNLOAD_SERVICE) as DownloadManager).enqueue(request)
            getSharedPreferences(UPDATE_PREFS, MODE_PRIVATE).edit().putLong(PENDING_DOWNLOAD_ID, id).apply()
            Toast.makeText(this, "Update downloading. Android will ask you to install it when ready.", Toast.LENGTH_LONG).show()
        } catch (_: Exception) {
            Toast.makeText(this, "Could not start the update download.", Toast.LENGTH_LONG).show()
        }
    }

    private fun installDownloadedUpdate(id: Long) {
        val uri = (getSystemService(DOWNLOAD_SERVICE) as DownloadManager).getUriForDownloadedFile(id)
        if (uri == null) {
            Toast.makeText(this, "Update file could not be opened.", Toast.LENGTH_LONG).show()
            return
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && !packageManager.canRequestPackageInstalls()) {
            AlertDialog.Builder(this)
                .setTitle("Allow MessMate update")
                .setMessage("Allow installs from this app in Android settings, then return to MessMate to finish updating.")
                .setNegativeButton("Later", null)
                .setPositiveButton("Settings") { _, _ ->
                    startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:$packageName")))
                }
                .show()
            return
        }
        val install = Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(uri, "application/vnd.android.package-archive")
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        try {
            startActivity(install)
            getSharedPreferences(UPDATE_PREFS, MODE_PRIVATE).edit().remove(PENDING_DOWNLOAD_ID).apply()
        } catch (_: Exception) {
            Toast.makeText(this, "Android could not open the installer.", Toast.LENGTH_LONG).show()
        }
    }

    private fun isTrustedOrigin(uri: Uri): Boolean = uri.scheme == "https" && uri.host == TRUSTED_HOST
    private fun isTrustedUrl(uri: Uri): Boolean = isTrustedOrigin(uri) || uri.scheme == "about"

    override fun onSaveInstanceState(outState: Bundle) {
        webView.saveState(outState)
        super.onSaveInstanceState(outState)
    }

    override fun onResume() {
        super.onResume()
        if (::webView.isInitialized && webView.url?.startsWith(APP_URL) == true) webView.reload()
        val pending = getSharedPreferences(UPDATE_PREFS, MODE_PRIVATE).getLong(PENDING_DOWNLOAD_ID, -1L)
        if (pending >= 0 && Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && packageManager.canRequestPackageInstalls()) installDownloadedUpdate(pending)
    }

    override fun onBackPressed() {
        if (::webView.isInitialized && webView.canGoBack()) webView.goBack() else super.onBackPressed()
    }

    override fun onDestroy() {
        try { unregisterReceiver(updateReceiver) } catch (_: Exception) { }
        if (::webView.isInitialized) {
            webView.stopLoading()
            webView.destroy()
        }
        super.onDestroy()
    }

    companion object {
        private const val TRUSTED_HOST = "messmate-zhms.onrender.com"
        private const val APP_URL = "https://messmate-zhms.onrender.com/mobile-scan.html"
        private const val LATEST_RELEASE_API = "https://api.github.com/repos/sparsh-ship-it-bit/MessMate/releases?per_page=20"
        private const val APK_ASSET_NAME = "MessMate-Android.apk"
        private const val UPDATE_PREFS = "messmate_updates"
        private const val PENDING_DOWNLOAD_ID = "pending_download_id"
    }
}
