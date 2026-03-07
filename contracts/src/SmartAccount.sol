// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.28;

import {BaseAccount} from "@account-abstraction/contracts/core/BaseAccount.sol";
import {IEntryPoint} from "@account-abstraction/contracts/interfaces/IEntryPoint.sol";
import {PackedUserOperation} from "@account-abstraction/contracts/interfaces/PackedUserOperation.sol";
import {SIG_VALIDATION_FAILED, SIG_VALIDATION_SUCCESS, _packValidationData} from "@account-abstraction/contracts/core/Helpers.sol";
import {ECDSA} from "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";
import {MessageHashUtils} from "@openzeppelin/contracts/utils/cryptography/MessageHashUtils.sol";
import {Initializable} from "@openzeppelin/contracts/proxy/utils/Initializable.sol";

/// @title SmartAccount
/// @notice ERC-4337 compliant smart account with ECDSA owner validation and session keys.
///         Designed to be deployed as a proxy via SmartAccountFactory (CREATE2).
contract SmartAccount is BaseAccount, Initializable {
    using ECDSA for bytes32;
    using MessageHashUtils for bytes32;

    // ───────────────────────────── Storage ─────────────────────────────

    /// @notice The EOA that owns this account and can sign UserOperations.
    address public owner;

    /// @notice The canonical EntryPoint v0.7 contract. Set once at construction
    ///         and shared across all proxies (lives in the implementation, not storage).
    IEntryPoint private immutable _ENTRY_POINT;

    /// @notice Configuration for a session key — scoped, time-bounded execution rights.
    struct SessionKeyData {
        address allowedTarget;
        bytes4 allowedSelector;
        uint48 validAfter;
        uint48 validUntil;
    }

    /// @notice Registered session keys. A key with `validUntil == 0` is not registered.
    mapping(address => SessionKeyData) public sessionKeys;

    // ───────────────────────────── Events ──────────────────────────────

    event SmartAccountInitialized(
        IEntryPoint indexed entryPoint,
        address indexed owner
    );

    /// @notice Emitted when the owner registers a new session key (exam spec name).
    event SessionKeyAdded(address indexed key, uint256 expiry);

    /// @notice Emitted when the owner revokes a session key (exam spec name).
    event SessionKeyRevoked(address indexed key);

    // ───────────────────────────── Errors ──────────────────────────────

    error OnlyOwnerOrEntryPoint();
    error OnlyOwner();
    error CallFailed(bytes returnData);
    error SessionKeyAlreadyRegistered(address key);
    error SessionKeyNotRegistered(address key);
    error InvalidSessionKeyParams();

    // ───────────────────────────── Modifiers ───────────────────────────

    /// @notice Restricts to the EntryPoint (during UserOp execution) or the owner (direct calls).
    modifier onlyOwnerOrEntryPoint() {
        _checkOwnerOrEntryPoint();
        _;
    }

    modifier onlyOwner() {
        _checkOwner();
        _;
    }

    // ─────────────────────────── Constructor ───────────────────────────

    /// @notice Sets the EntryPoint. Called once on the *implementation* contract.
    ///         `_disableInitializers()` prevents anyone from calling `initialize`
    ///         on the implementation itself — only proxies can be initialized.
    /// @param entryPoint_ The EntryPoint v0.7 address (same on all EVM chains).
    constructor(IEntryPoint entryPoint_) {
        _ENTRY_POINT = entryPoint_;
        _disableInitializers();
    }

    // ─────────────────────────── Initializer ───────────────────────────

    /// @notice Initializes a proxy clone. Called once by the factory during CREATE2 deployment.
    /// @param owner_ The EOA that will own this smart account.
    function initialize(address owner_) external initializer {
        require(owner_ != address(0), "invalid owner");
        owner = owner_;
        emit SmartAccountInitialized(_ENTRY_POINT, owner_);
    }

    // ──────────────────── BaseAccount implementation ───────────────────

    /// @inheritdoc BaseAccount
    function entryPoint() public view override returns (IEntryPoint) {
        return _ENTRY_POINT;
    }

    /// @notice Validates the UserOperation signature against the owner or a session key.
    ///
    ///         Flow:
    ///         1. Recover the signer from the signature (tryRecover — never reverts).
    ///         2. If signer == owner → return success (owner can do anything).
    ///         3. If signer is a registered session key → parse userOp.callData to enforce
    ///            target + selector restrictions, return packed validationData with time bounds.
    ///         4. Otherwise → return SIG_VALIDATION_FAILED.
    ///
    ///         Session key enforcement happens here (not in execute) because userOp.callData
    ///         is signed — it cannot be tampered with between validation and execution.
    ///
    /// @inheritdoc BaseAccount
    function _validateSignature(
        PackedUserOperation calldata userOp,
        bytes32 userOpHash
    ) internal view override returns (uint256 validationData) {
        bytes32 ethSignedHash = userOpHash.toEthSignedMessageHash();

        (address recovered, ECDSA.RecoverError err, ) = ethSignedHash
            .tryRecover(userOp.signature);
        if (err != ECDSA.RecoverError.NoError) {
            return SIG_VALIDATION_FAILED;
        }

        if (recovered == owner) {
            return SIG_VALIDATION_SUCCESS;
        }

        return _validateSessionKey(recovered, userOp.callData);
    }

    // ──────────────────── Session key management ───────────────────────

    /// @notice Register a new session key. Only the owner can call this.
    /// @param key             The session key address (will sign UserOps).
    /// @param allowedTarget   The contract this key may call.
    /// @param allowedSelector The function selector this key may invoke.
    /// @param validAfter      Timestamp after which the key is active.
    /// @param validUntil      Timestamp after which the key expires. Must be > 0.
    function registerSessionKey(
        address key,
        address allowedTarget,
        bytes4 allowedSelector,
        uint48 validAfter,
        uint48 validUntil
    ) external onlyOwner {
        if (
            key == address(0) ||
            allowedTarget == address(0) ||
            allowedSelector == bytes4(0) ||
            validUntil == 0 ||
            validUntil <= validAfter
        ) {
            revert InvalidSessionKeyParams();
        }
        if (sessionKeys[key].validUntil != 0) {
            revert SessionKeyAlreadyRegistered(key);
        }

        sessionKeys[key] = SessionKeyData({
            allowedTarget: allowedTarget,
            allowedSelector: allowedSelector,
            validAfter: validAfter,
            validUntil: validUntil
        });

        emit SessionKeyAdded(key, validUntil);
    }

    /// @notice Revoke a session key. Only the owner can call this.
    /// @param key The session key address to revoke.
    function revokeSessionKey(address key) external onlyOwner {
        if (sessionKeys[key].validUntil == 0) {
            revert SessionKeyNotRegistered(key);
        }

        delete sessionKeys[key];
        emit SessionKeyRevoked(key);
    }

    // ──────────────────────────── Execution ────────────────────────────

    /// @notice Execute a single call from this account.
    ///         Called by the EntryPoint after successful validation (via userOp.callData),
    ///         or directly by the owner for convenience.
    /// @param target  The contract (or EOA) to call.
    /// @param value   The ETH value to send.
    /// @param data    The calldata (function selector + arguments).
    function execute(
        address target,
        uint256 value,
        bytes calldata data
    ) external onlyOwnerOrEntryPoint {
        (bool success, bytes memory returnData) = target.call{value: value}(
            data
        );
        if (!success) {
            revert CallFailed(returnData);
        }
    }

    /// @notice Execute multiple calls in a single UserOperation.
    ///         Useful for batching (e.g., approve + swap in one op).
    /// @param targets  Array of addresses to call.
    /// @param values   Array of ETH values to send.
    /// @param calldatas Array of calldata payloads.
    function executeBatch(
        address[] calldata targets,
        uint256[] calldata values,
        bytes[] calldata calldatas
    ) external onlyOwnerOrEntryPoint {
        require(
            targets.length == values.length &&
                values.length == calldatas.length,
            "length mismatch"
        );
        for (uint256 i = 0; i < targets.length; i++) {
            (bool success, bytes memory returnData) = targets[i].call{
                value: values[i]
            }(calldatas[i]);
            if (!success) {
                revert CallFailed(returnData);
            }
        }
    }

    // ───────────────────────────── Receive ─────────────────────────────

    /// @notice Accept ETH deposits. The account needs ETH to pay for gas
    ///         (deposited to the EntryPoint, or held directly).
    receive() external payable {}

    // ──────────────────────── Internal helpers ─────────────────────────

    /// @notice Validates a session key's permissions by parsing userOp.callData.
    ///
    ///         callData layout for execute(address,uint256,bytes):
    ///         [0:4]     — execute selector (0xb61d27f6)
    ///         [4:36]    — target address (left-padded to 32 bytes)
    ///         [36:68]   — value (uint256)
    ///         [68:100]  — offset to bytes data (must be 0x60 for canonical encoding)
    ///         [100:132] — length of bytes data
    ///         [132:136] — inner function selector (first 4 bytes of data)
    ///
    ///         Security invariants:
    ///         - Offset must equal 0x60 (canonical ABI encoding). Non-canonical offsets
    ///           would let an attacker place the allowed selector where we check while
    ///           encoding a different selector where Solidity actually decodes.
    ///         - Value must be 0. Session keys cannot transfer ETH.
    ///         - data.length (at [100:132]) must be >= 4 and callData must be long enough
    ///           to contain it. Otherwise an attacker could set length to 0, append trailing
    ///           bytes with the allowed selector, and have execute forward empty calldata.
    ///
    /// @param signer  The recovered session key address.
    /// @param callData The full userOp.callData.
    /// @return validationData Packed (sigFailed, validUntil, validAfter) or SIG_VALIDATION_FAILED.
    function _validateSessionKey(
        address signer,
        bytes calldata callData
    ) internal view returns (uint256) {
        SessionKeyData storage sk = sessionKeys[signer];

        if (sk.validUntil == 0) {
            return SIG_VALIDATION_FAILED;
        }

        // Session keys can only call execute — not executeBatch or anything else.
        // Minimum length: 4 (selector) + 3×32 (head) = 100 bytes.
        if (
            callData.length < 100 ||
            bytes4(callData[:4]) != this.execute.selector
        ) {
            return SIG_VALIDATION_FAILED;
        }

        address target = address(bytes20(callData[16:36]));

        if (target != sk.allowedTarget) {
            return SIG_VALIDATION_FAILED;
        }

        // Session keys cannot transfer ETH — enforce value == 0.
        if (uint256(bytes32(callData[36:68])) != 0) {
            return SIG_VALIDATION_FAILED;
        }

        // Reject non-canonical ABI encoding. The dynamic `bytes` offset must be
        // exactly 0x60 (96) for execute(address,uint256,bytes) — three 32-byte
        // head slots. A different offset would let an attacker place the allowed
        // selector at [132:136] while Solidity decodes from a different position.
        if (uint256(bytes32(callData[68:100])) != 0x60) {
            return SIG_VALIDATION_FAILED;
        }

        // Validate the ABI-encoded data length. Without this check, an attacker
        // could set data.length to 0 while appending trailing bytes past position 132.
        // Our selector check would read the trailing bytes and pass, but Solidity's
        // abi.decode would see length 0 and forward empty calldata to the target
        // (hitting its fallback/receive instead of the allowed function).
        uint256 dataLen = uint256(bytes32(callData[100:132]));
        if (dataLen < 4 || callData.length < 132 + dataLen) {
            return SIG_VALIDATION_FAILED;
        }

        bytes4 innerSelector = bytes4(callData[132:136]);
        if (innerSelector != sk.allowedSelector) {
            return SIG_VALIDATION_FAILED;
        }

        return _packValidationData(false, sk.validUntil, sk.validAfter);
    }

    function _checkOwnerOrEntryPoint() internal view {
        if (msg.sender != address(entryPoint()) && msg.sender != owner) {
            revert OnlyOwnerOrEntryPoint();
        }
    }

    function _checkOwner() internal view {
        if (msg.sender != owner) {
            revert OnlyOwner();
        }
    }
}
