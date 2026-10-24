package com.messmate.mobile

import android.Manifest
import android.annotation.SuppressLint
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Bundle
import android.webkit.PermissionRequest
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.webkit.WebSettings
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat

class MainActivity : AppCompatActivity() {
    private lateinit var webView: WebView
    private var pendingWebPermission: PermissionRequest? = null

    private val cameraPermissionLauncher =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
            val request = pendingWebPermission
            pendingWebPermission = null
            if (granted && request != null && isTrustedOrigin(request.origin)) {
                request.grant(arrayOf(PermissionRequest.RESOURCE_VIDEO_CAPTURE))
            } else {
                request?.deny()
            }
        }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.statusBarColor = android.graphics.Color.rgb(5, 5, 5)
        window.navigationBarColor = android.graphics.Color.rgb(5, 5, 5)

        webView = WebView(this)
        webView.settings.javaScriptEnabled = true
        webView.settings.domStorageEnabled = true
        webView.settings.cacheMode = WebSettings.LOAD_NO_CACHE
        webView.settings.mediaPlaybackRequiresUserGesture = true
        webView.settings.allowFileAccess = false
        webView.settings.allowContentAccess = false
        webView.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                val uri = request.url
                return !isTrustedUrl(uri)
            }
        }
        webView.webChromeClient = object : WebChromeClient() {
            override fun onPermissionRequest(request: PermissionRequest) {
                runOnUiThread {
                    if (!isTrustedOrigin(request.origin) ||
                        request.resources.none { it == PermissionRequest.RESOURCE_VIDEO_CAPTURE }) {
                        request.deny()
                        return@runOnUiThread
                    }
                    if (ContextCompat.checkSelfPermission(
                            this@MainActivity, Manifest.permission.CAMERA
                        ) == PackageManager.PERMISSION_GRANTED) {
                        request.grant(arrayOf(PermissionRequest.RESOURCE_VIDEO_CAPTURE))
                    } else {
                        pendingWebPermission = request
                        cameraPermissionLauncher.launch(Manifest.permission.CAMERA)
                    }
                }
            }
        }
        setContentView(webView)
        if (savedInstanceState == null) {
            webView.loadUrl(APP_URL)
        } else {
            webView.restoreState(savedInstanceState)
        }
    }

    private fun isTrustedOrigin(uri: Uri): Boolean =
        uri.scheme == "https" && uri.host == TRUSTED_HOST

    private fun isTrustedUrl(uri: Uri): Boolean =
        isTrustedOrigin(uri) || uri.scheme == "about"

    override fun onSaveInstanceState(outState: Bundle) {
        webView.saveState(outState)
        super.onSaveInstanceState(outState)
    }

    override fun onResume() {
        super.onResume()
        if (::webView.isInitialized && webView.url?.startsWith(APP_URL) == true) webView.reload()
    }

    override fun onBackPressed() {
        if (::webView.isInitialized && webView.canGoBack()) webView.goBack()
        else super.onBackPressed()
    }

    override fun onDestroy() {
        if (::webView.isInitialized) {
            webView.stopLoading()
            webView.destroy()
        }
        super.onDestroy()
    }

    companion object {
        private const val TRUSTED_HOST = "messmate-zhms.onrender.com"
        private const val APP_URL = "https://messmate-zhms.onrender.com/mobile-scan.html"
    }
}
